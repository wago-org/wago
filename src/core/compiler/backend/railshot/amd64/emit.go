//go:build amd64

package amd64

// The condense engine: materialize a deferred-action valent block into machine
// code, with target hints (compute the result straight into a destination
// register and reuse operand registers in place). Ported from WARP's
// condenseValentBlock / emitDeferredAction / selectInstr operand folding.

// aluEnc holds the x86 encoding bytes for a binary integer ALU op, matching the
// amd64 encoder's opcode/digit conventions: rr = reg,reg form; rm = reg,mem
// form; digit = /n extension for reg,imm.
type aluEnc struct {
	rr, rm, digit byte
	comm          bool
}

var aluTable = map[wOp]aluEnc{
	opAdd: {0x01, 0x03, 0, true},
	opSub: {0x29, 0x2B, 5, false},
	opAnd: {0x21, 0x23, 4, true},
	opOr:  {0x09, 0x0B, 1, true},
	opXor: {0x31, 0x33, 6, true},
}

// x86 CMP encodings (used by the compare path): reg,imm via /7; reg,r/m via 0x3B.
const (
	cmpDigit  = 7
	cmpRMcode = 0x3B
)

// condense emits the deferred node's value into a register. dest is a target
// register the result must land in (target hint, e.g. a local's register) or
// regNone to pick a fresh one. Returns the register now holding the value and
// converts `node` into that value on the stack (its operands are consumed).
func (f *fn) condense(node *elem, dest Reg) Reg {
	f.stats.addCondense()
	switch {
	case isBinALU(node.op):
		return f.condenseBinary(node, dest)
	case isShift(node.op):
		return f.condenseShift(node, dest)
	case isCompare(node.op) || node.op == opEqz:
		return f.condenseCompare(node, dest)
	case isUnary(node.op):
		return f.condenseUnary(node, dest)
	case isConvert(node.op):
		return f.condenseConvert(node, dest)
	case isDivRem(node.op):
		return f.condenseDivRem(node, dest)
	}
	panic("amd64: unsupported deferred op")
}

// condenseConvert lowers the integer width conversions (wrap / sign- & zero-
// extend). Each reads the source register and writes the converted value; the
// source register can be reused when there is no target hint.
// producesCleanI32 reports whether an i32-typed deferred op materializes into a
// register whose upper 32 bits are guaranteed zero. All of these lower to 32-bit
// instructions (ALU/shift/mul/div, bit counts) or a 0/1 setcc, and a 32-bit write
// clears the upper half on x86-64. Loads and local/global reads are excluded:
// they can surface dirty upper bits (garbage-padded params, sign-extending loads).
func producesCleanI32(op wOp) bool {
	switch op {
	case opAdd, opSub, opAnd, opOr, opXor,
		opShl, opShrU, opShrS, opRotl, opRotr,
		opMul, opDivU, opDivS, opRemU, opRemS,
		opClz, opCtz, opPopcnt,
		opEq, opNe, opLtS, opLtU, opGtS, opGtU, opLeS, opLeU, opGeS, opGeU, opEqz,
		opWrap, opZExt32:
		return true
	}
	return false
}

// elemCleanUpper reports whether the value elem e will produce has its upper 32
// bits known zero. Conservative — returns false unless proven. A deferred i32 op
// is clean iff producesCleanI32; an i32 const whose stored int64 already has a
// zero upper half is clean; an i32 local is clean per perLocalClean (which tracks
// the cleanliness of whatever was last stored into it).
func (f *fn) elemCleanUpper(e *elem) bool {
	if e == nil {
		return false
	}
	if e.kind == ekDeferred {
		return e.typ == mtI32 && producesCleanI32(e.op)
	}
	if e.st.typ != mtI32 {
		return false
	}
	switch e.st.kind {
	case stConst:
		return e.st.cval&^0xFFFFFFFF == 0
	case stLocalReg, stLocalRef:
		return cleanUpperEnabled && e.st.idx < len(f.perLocalClean) && f.perLocalClean[e.st.idx]
	}
	return false
}

func (f *fn) condenseConvert(node *elem, dest Reg) Reg {
	// Redundant zero-extend elimination: i64.extend_i32_u of a value already in
	// clean zero-upper form (an i32 produced by a 32-bit instruction, which zeroes
	// the upper 32 bits on x86-64) is a no-op. Captured before materialize consumes
	// the deferred node. NOT applied to i32 locals/params or sign-extending loads,
	// which can carry dirty upper bits — hence the producer-op whitelist.
	cleanZExt := node.op == opZExt32 && node.arg0.typ == mtI32 &&
		((node.arg0.kind == ekDeferred && producesCleanI32(node.arg0.op)) ||
			// A value that reached an i32 local proven clean (perLocalClean) and is
			// read back from its frame slot (stLocalRef → an owned Load64) needs no
			// re-zeroing. Restricted to the spilled ref: the Load64 yields an owned
			// register, so eliding the mov cannot alias a pinned local, and a pinned
			// local's extend already folds copy+zero into one mov anyway.
			(cleanUpperEnabled && node.arg0.kind == ekValue &&
				node.arg0.st.kind == stLocalRef && node.arg0.st.idx < len(f.perLocalClean) &&
				f.perLocalClean[node.arg0.st.idx]))
	src := f.materialize(node.arg0)
	result := src
	if dest != regNone && dest != src {
		result = dest
	}
	switch node.op {
	case opZExt32:
		if cleanZExt && result == src {
			f.stats.peep("ext-elim") // upper 32 already zero; the mov would be a no-op
			break
		}
		f.a.MovRegReg32(result, src) // 32-bit mov zero-extends into the full register
	case opWrap:
		f.a.MovRegReg32(result, src)
	case opSExt32:
		f.a.Movsxd(result, src)
	case opSExt8:
		f.a.Movsx8(result, src, node.typ.is64())
	case opSExt16:
		f.a.Movsx16(result, src, node.typ.is64())
	}
	if result != src {
		f.release(src)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.op = opNone
	return result
}

// condenseBinary handles the straight two-operand ALU ops (add/sub/and/or/xor)
// and mul: compute the left operand into dest, then fold the right operand in
// place (const→imm, memory→r/m, reg→reg).
func (f *fn) condenseBinary(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	left := node.arg0
	right := node.arg1

	// Commutative reassociation (selectInstr): if the left operand is a constant
	// but the right is not, swap so the constant folds as an immediate rather than
	// being loaded into dest.
	if node.op.commutative() &&
		left.kind == ekValue && left.st.kind == stConst &&
		!(right.kind == ekValue && right.st.kind == stConst) {
		left, right = right, left
	}

	// Scaled-index fusion: add(x, shl(y, k∈1..3)) → `lea dest,[x + y*2ᵏ]` — one
	// instruction replacing shl+add. The common AssemblyScript array-address
	// shape (`base + (i << log2size)`).
	if node.op == opAdd {
		if r := f.tryLeaScaledAdd(node, left, right, dest); r != regNone {
			return r
		}
	}

	// Strength-reduce x * {3,5,9} to a single LEA `[x + x*{2,4,8}]` (base == index
	// == x), replacing an IMUL by a small constant. The multiplier sits on the
	// right after the commutative swap above.
	if node.op == opMul {
		if r := f.tryLeaMul(node, left, right, dest); r != regNone {
			return r
		}
	}

	// Materialize the RHS into a safe, foldable operand BEFORE the LHS overwrites
	// dest: condense a deferred RHS to a fresh register, and copy a register RHS
	// out if it aliases dest.
	rightReleaseAfter := regNone
	pinnedRight := regNone
	if right.isDeferred() {
		rr := f.condense(right, regNone)
		// Computing the LHS next can clobber the just-condensed RHS register: it
		// writes into `dest` (so a RHS that landed in dest is lost — e.g. a div
		// consumer passes dest=RAX and the RHS div result also lands in RAX), and a
		// deferred LHS may be a div/rem or shift that hard-targets RAX/RDX or RCX
		// regardless of dest and pins. In either case the LHS op spills its own
		// node, not this detached operand, so the corruption goes silently. Relocate
		// the RHS to a scratch register clear of all those and pin it across the LHS.
		fixedHazard := left.isDeferred() && (rr == RAX || rr == RDX || rr == RCX)
		if fixedHazard || (dest != regNone && rr == dest) {
			avoid := maskOf(RAX, RDX, RCX)
			if dest != regNone {
				avoid = avoid.union(maskOf(dest))
			}
			if safe := f.allocRegOrNone(avoid); safe != regNone {
				f.a.MovReg64(safe, rr)
				f.release(rr)
				rr = safe
				f.pinned = f.pinned.add(rr)
				pinnedRight = rr
				right = &elem{kind: ekValue, st: storage{kind: stReg, typ: node.typ, reg: rr}}
				rightReleaseAfter = rr
			} else {
				// Nested hazards can exhaust the hazard-free registers (each level
				// pins one relocated RHS). Park this RHS in a tracked spill slot
				// instead — the ALU folds it back as an r/m operand. `right` is the
				// on-stack condensed node, so the slot stays visible to the
				// allocator until consumeBlockBelow erases it.
				f.spill(right)
			}
		}
		// Otherwise leave `right` as the condensed on-stack node (do NOT detach into
		// a {stReg: rr} copy): computing the LHS can spill it under register pressure
		// — reached most readily via a load's inline bounds check (explicit mode),
		// whose allocReg reclaims rr — and applyALU then folds it back from its spill
		// slot. A detached copy would instead read rr after the spill freed and reused
		// it, silently reading a clobbered register (the inflate/flush_block
		// explicit-mode miscompile: the i64 bit-buffer OR). The on-stack node tracks
		// the spill; consumeBlockBelow erases it and applyALU releases its register.
		// Mirrors the spill-fallback in the relocate branch above.
	} else if (right.st.kind == stReg || right.st.kind == stLocalReg || right.st.kind == stGlobReg) && dest != regNone && right.st.reg == dest {
		// In-place self-update (e.g. `x = (a<<b) | x`): the old RHS lives in dest,
		// which computing the LHS will overwrite. Spill it to a slot so applyALU
		// folds it from memory. Copying it into a scratch register and computing the
		// LHS through that scratch instead risks the scratch being reused under
		// register pressure (an unpinned copy → the guard-page miscompile), while
		// pinning the copy perturbs the allocator's register choices for unrelated
		// values (a two-consumer value desynced → a quicksort miscompile). Spilling
		// touches no register, so it is safe on both counts and folds as an r/m
		// operand. (The inflate/flush_block guard-page miscompile: `L11 = (…) | L11`.)
		f.spill(right)
	}

	if dest == regNone {
		// selectInstr forms (choose the cheapest emission):
		//  - LEA add:  `lea dst, [local + reg|imm]` computes local+x in one insn
		//    without clobbering the pinned local (which reg-reg add would require a
		//    preceding copy for).
		//  - in-place: reuse an owned-register left as the destination, so the op
		//    accumulates in place with no preceding mov.
		if node.op == opAdd && left.kind == ekValue && (left.st.kind == stLocalReg || left.st.kind == stGlobReg) && leaRightOK(right) {
			dest = f.allocReg(0)
			f.emitLeaAdd(dest, left.st.reg, right, w)
			f.release(rightReleaseAfter)
			f.consumeBlockBelow(node)
			f.occupy(node, dest)
			node.op = opNone
			return dest
		}
		if left.kind == ekValue && left.st.kind == stReg {
			dest = left.st.reg // in-place accumulate (no mov)
		} else {
			dest = f.allocReg(0)
		}
	}
	f.pinned = f.pinned.add(dest)
	f.condenseInto(left, dest)
	if node.op == opMul {
		f.applyMul(dest, right, w)
	} else {
		f.applyALU(aluTable[node.op], dest, right, w)
	}
	f.pinned = f.pinned.remove(dest)
	if pinnedRight != regNone {
		f.pinned = f.pinned.remove(pinnedRight)
	}
	f.release(rightReleaseAfter)

	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return dest
}

// shlByConst123 reports whether e is a deferred shl of node-typ t by a constant
// masked count in 1..3 (an LEA-encodable scale), returning the count.
func shlByConst123(e *elem, t machineType) (int, bool) {
	if e == nil || e.kind != ekDeferred || e.op != opShl || e.typ != t {
		return 0, false
	}
	c := e.arg1
	if c == nil || c.kind != ekValue || c.st.kind != stConst {
		return 0, false
	}
	mask := int64(31)
	if t.is64() {
		mask = 63
	}
	if k := c.st.cval & mask; k >= 1 && k <= 3 {
		return int(k), true
	}
	return 0, false
}

// tryLeaScaledAdd lowers add(x, shl(y,k)) (either operand order) as a single
// scaled-index LEA. Returns the result register, or regNone when the shape
// doesn't match.
func (f *fn) tryLeaScaledAdd(node, left, right *elem, dest Reg) Reg {
	w := node.typ.is64()
	shl, other := right, left
	k, ok := shlByConst123(shl, node.typ)
	if !ok {
		shl, other = left, right
		if k, ok = shlByConst123(shl, node.typ); !ok {
			return regNone
		}
	}
	// Only fuse when both the base and the shifted value are already concrete
	// values: condensing a deferred operand here could hard-clobber RAX/RDX/RCX
	// (div/shift) underneath the other operand. The AS address shape
	// (local/global base + local index) is concrete by the time the add is built.
	if other.kind != ekValue || shl.arg0 == nil || shl.arg0.kind != ekValue {
		return regNone
	}
	// The index: read a pinned local in place (LEA never writes its sources);
	// anything else materializes into an owned register.
	y, yOwned := f.materializeRead(shl.arg0)
	f.pinned = f.pinned.add(y) // survive the base's materialization
	x, xOwned := f.materializeRead(other)
	f.pinned = f.pinned.remove(y)
	if dest == regNone {
		switch {
		case xOwned:
			dest = x // reuse the owned base in place
		case yOwned:
			dest = y
		default:
			dest = f.allocReg(0)
		}
	}
	f.stats.peep("lea-scaled-index")
	f.a.LeaScaledW(dest, x, y, uint8(k), 0, w)
	if yOwned && y != dest {
		f.release(y)
	}
	if xOwned && x != dest {
		f.release(x)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return dest
}

// tryLeaMul lowers x * {3,5,9} as a single LEA `dest = [x + x*{2,4,8}]` (base ==
// index == x), replacing an IMUL by a small constant. Returns regNone when the
// shape doesn't match. The multiplicand must be concrete: condensing a deferred
// operand here could hard-clobber RAX/RDX/RCX under the LEA (same hazard as
// tryLeaScaledAdd guards against).
func (f *fn) tryLeaMul(node, left, right *elem, dest Reg) Reg {
	if right.kind != ekValue || right.st.kind != stConst {
		return regNone
	}
	var scaleLog uint8
	switch right.st.cval {
	case 3:
		scaleLog = 1
	case 5:
		scaleLog = 2
	case 9:
		scaleLog = 3
	default:
		return regNone
	}
	if left.kind != ekValue {
		return regNone
	}
	w := node.typ.is64()
	x, xOwned := f.materializeRead(left) // LEA never writes its sources; a pinned local reads in place
	if dest == regNone {
		if xOwned {
			dest = x // reuse the owned multiplicand in place
		} else {
			dest = f.allocReg(0)
		}
	}
	f.a.LeaScaledW(dest, x, x, scaleLog, 0, w)
	if xOwned && x != dest {
		f.release(x)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return dest
}

// leaRightOK reports whether the right add operand can be an LEA index/displacement.
func leaRightOK(right *elem) bool {
	if right.kind != ekValue {
		return false
	}
	switch right.st.kind {
	case stReg, stLocalReg, stGlobReg:
		return true
	case stConst:
		return fitsImm32(right.st.cval)
	}
	return false
}

// emitLeaAdd emits `dst = base + right` via LEA (base is a register-resident value
// that must be preserved). Releases an owned register right.
func (f *fn) emitLeaAdd(dst, base Reg, right *elem, w bool) {
	switch right.st.kind {
	case stConst:
		f.a.LeaDispW(dst, base, int32(right.st.cval), w)
	case stReg:
		f.a.LeaScaledW(dst, base, right.st.reg, 0, 0, w)
		f.release(right.st.reg)
	case stLocalReg, stGlobReg:
		f.a.LeaScaledW(dst, base, right.st.reg, 0, 0, w) // pinned local/global; never released
	}
}

// condenseShift lowers shl/shr_s/shr_u/rotl/rotr. A constant count folds to an
// immediate shift; a variable count must live in CL (x86 constraint), so it is
// forced into RCX and the value is shifted by CL.
func (f *fn) condenseShift(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	digit := shiftDigit(node.op)
	left := node.arg0
	right := node.arg1

	if right.kind == ekValue && right.st.kind == stConst {
		if dest == regNone {
			dest = f.allocReg(0)
		}
		f.pinned = f.pinned.add(dest)
		f.condenseInto(left, dest)
		mask := int64(31)
		if w {
			mask = 63
		}
		f.a.ShiftImm(digit, dest, byte(right.st.cval&mask), w)
		f.pinned = f.pinned.remove(dest)
		f.consumeBlockBelow(node)
		f.occupy(node, dest)
		node.op = opNone
		return dest
	}

	// Variable count → CL. Compute the shifted value into a scratch register that
	// no sub-computation hard-targets — not RAX/RDX (a div/rem operand may appear
	// in `left` or `right`) and not RCX (the count, or a nested variable shift).
	// A caller-supplied `dest` can itself be such a fixed register (e.g. RAX when a
	// div consumes this shift), so shift in the neutral scratch and move to dest at
	// the end. Evaluate left before right (wasm order).
	val := f.allocReg(maskOf(RAX, RDX, RCX))
	f.pinned = f.pinned.add(val)
	f.condenseInto(left, val)
	cnt := f.materialize(right)
	if cnt != RCX {
		f.spillIfUsed(RCX)
		f.a.MovReg64(RCX, cnt)
		f.release(cnt)
	}
	f.pinned = f.pinned.add(RCX)
	f.a.ShiftCL(digit, val, w)
	f.pinned = f.pinned.remove(RCX)
	f.release(RCX)
	f.pinned = f.pinned.remove(val)
	result := val
	if dest != regNone && dest != val {
		f.a.MovReg64(dest, val)
		f.release(val)
		result = dest
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.op = opNone
	return result
}

// condenseCompare lowers the relational ops and eqz to a CMP/TEST + SETcc,
// producing a 0/1 i32 result. (Fusing compares directly into branches is a later
// optimization; Phase 1 materializes the boolean.)
func (f *fn) condenseCompare(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	left := node.arg0

	// cmp/test read the left comparand read-only, so a borrowed pinned-local/global
	// register can feed the compare in place — no copy — provided nothing between
	// the read and the compare writes it. That holds for eqz and for a constant
	// right operand (the compare is emitted immediately, the only intervening emit
	// being a const load into a fresh temp). For any other right operand we keep the
	// copy: a deferred right could be condensed here and clobber, and comparing the
	// live pinned register afterwards would read a post-write value. When L is a
	// borrowed register the trailing setcc must not clobber it, so the boolean lands
	// in a separate register (dest or a fresh temp) instead of reusing L.
	inPlaceOK := node.op == opEqz ||
		(node.arg1.kind == ekValue && node.arg1.st.kind == stConst)
	var L Reg
	ownL := true
	if inPlaceOK {
		L, ownL = f.materializeRead(left)
	} else {
		L = f.materialize(left)
	}
	f.pinned = f.pinned.add(L)

	var cc Cond
	if node.op == opEqz {
		cc = condE
		f.a.TestSelf(L, w)
	} else {
		cc = condOf(node.op)
		right := node.arg1
		if right.isDeferred() {
			rr := f.condense(right, regNone)
			right = &elem{kind: ekValue, st: storage{kind: stReg, typ: node.typ, reg: rr}}
		}
		switch right.st.kind {
		case stConst:
			if fitsImm32(right.st.cval) {
				f.a.AluRI(cmpDigit, L, int32(right.st.cval), w)
			} else {
				t := f.allocReg(maskOf(L))
				f.loadConst(t, right.st)
				f.cmpRR(L, t, w)
				f.release(t)
			}
		case stReg:
			f.cmpRR(L, right.st.reg, w)
			f.release(right.st.reg)
		case stLocalReg, stGlobReg:
			f.cmpRR(L, right.st.reg, w) // pinned local/global; never release
		case stSlot:
			f.a.AluRM(cmpRMcode, L, RSP, f.spillOff(right.st.slot), w)
		case stLocalRef:
			f.a.AluRM(cmpRMcode, L, RSP, f.localOff(right.st.idx), w)
		case stMemRef:
			if memRefFoldable(right.st, w) {
				f.a.AluIdx(cmpRMcode, L, RBX, right.st.reg, right.st.memDisp(), w)
			} else {
				f.loadMemRef(right.st.reg, right.st)
				f.cmpRR(L, right.st.reg, w)
			}
			f.release(right.st.reg)
		}
	}
	f.pinned = f.pinned.remove(L)

	// Choose the register the setcc boolean lands in. If we own L (a scratch temp),
	// reuse it in place. If L is a borrowed pinned register, it must survive, so the
	// result goes to dest or a fresh temp (never L).
	var result Reg
	switch {
	case dest != regNone:
		result = dest
		if ownL && dest != L {
			f.release(L)
		}
	case ownL:
		result = L
	default:
		result = f.allocReg(maskOf(L)) // borrowed L: fresh reg, guaranteed != L
	}
	// A compare materialized to a 0/1 boolean instead of fused into a branch —
	// the stFlags opportunity (no-ir-plan P3). Counting it quantifies how much a
	// flags-resident compare result would save before building it.
	f.stats.peep("compare-setcc")
	f.a.SetccReg(cc, result)
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.st.typ = mtI32 // relational result is always i32
	node.op = opNone
	return result
}

// condenseUnary lowers clz/ctz/popcnt (lzcnt/tzcnt/popcnt reg,reg).
func (f *fn) condenseUnary(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	// lzcnt/tzcnt/popcnt read their source read-only, so a register-resident source
	// (a pinned local or owned temp) can feed the op directly — no copy.
	arg := node.arg0
	var src Reg
	srcOwned := true
	if arg.kind == ekValue && (arg.st.kind == stLocalReg || arg.st.kind == stGlobReg) {
		src, srcOwned = arg.st.reg, false // pinned local/global: read directly, never release
	} else {
		src = f.materialize(arg)
	}

	result := dest
	if result == regNone {
		if srcOwned {
			result = src // reuse the owned temp in place
		} else {
			result = f.allocReg(0)
		}
	}
	switch node.op {
	case opClz:
		f.a.Lzcnt(result, src, w)
	case opCtz:
		f.a.Tzcnt(result, src, w)
	case opPopcnt:
		f.a.Popcnt(result, src, w)
	}
	if srcOwned && result != src {
		f.release(src)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.op = opNone
	return result
}

// condenseDivRem lowers div_s/div_u/rem_s/rem_u using x86's fixed RDX:RAX / RAX
// (quotient) / RDX (remainder) registers, with the two wasm-mandated integer
// division traps: divide-by-zero (all four ops) and the signed INT_MIN/-1
// overflow (div_s only; rem_s must instead yield 0 without faulting).
func (f *fn) condenseDivRem(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	signed := node.op == opDivS || node.op == opRemS
	wantRem := node.op == opRemS || node.op == opRemU
	left := node.arg0
	right := node.arg1

	// Constant divisor: strength-reduce to shifts / multiply-high, avoiding idiv.
	if right.kind == ekValue && right.st.kind == stConst {
		if r, ok := f.tryDivByConst(node, dest, right.st.cval); ok {
			return r
		}
	}

	// Reserve RAX (dividend/quotient) and RDX (high half/remainder).
	f.spillIfUsed(RAX)
	f.spillIfUsed(RDX)
	f.pinned = f.pinned.add(RAX)
	f.pinned = f.pinned.add(RDX)

	// Divisor into any non-RAX/RDX register: those hold the dividend and the
	// high-half/remainder during the divide, so the divisor must live elsewhere or
	// Cdq/XorSelf32 would corrupt it. materialize does not honor that constraint —
	// and if `right` is itself a div/rem its result lands in RAX/RDX and its own
	// reservation clears our pins — so re-assert RAX/RDX and relocate if needed.
	divisor := f.materialize(right)
	f.pinned = f.pinned.add(RAX)
	f.pinned = f.pinned.add(RDX)
	if divisor == RAX || divisor == RDX {
		safe := f.allocReg(0) // avoids the (re-)pinned RAX/RDX
		f.a.MovReg64(safe, divisor)
		f.occupy(right, safe)
		divisor = safe
	}
	f.pinned = f.pinned.add(divisor)

	// Dividend into RAX.
	f.condenseInto(left, RAX)

	// Divide-by-zero traps for every division op.
	f.a.TestSelf(divisor, w)
	f.trapIf(condE, trapDivZero)

	switch {
	case signed && !wantRem: // div_s: INT_MIN / -1 would raise #DE — trap it as overflow
		f.a.AluRI(7, divisor, -1, w) // cmp divisor, -1
		noOvf := f.a.JccPlaceholder(condNE)
		f.cmpIntMin(w) // cmp dividend (RAX), INT_MIN
		f.trapIf(condE, trapDivOverflow)
		f.a.PatchRel32(noOvf, f.a.Len())
		f.a.Cdq(w) // sign-extend RAX → RDX:RAX
		f.a.Idiv(divisor, w)
	case signed: // rem_s: x % -1 == 0, computed directly to avoid the #DE on INT_MIN/-1
		f.a.AluRI(7, divisor, -1, w) // cmp divisor, -1
		notM1 := f.a.JccPlaceholder(condNE)
		f.a.XorSelf32(RDX) // remainder is 0
		done := f.a.JmpPlaceholder()
		f.a.PatchRel32(notM1, f.a.Len())
		f.a.Cdq(w)
		f.a.Idiv(divisor, w)
		f.a.PatchRel32(done, f.a.Len())
	default: // div_u / rem_u
		f.a.XorSelf32(RDX) // zero RDX (clears upper 64 too)
		f.a.Div(divisor, w)
	}

	src := RAX
	if wantRem {
		src = RDX
	}
	f.pinned = f.pinned.remove(RAX)
	f.pinned = f.pinned.remove(RDX)
	f.pinned = f.pinned.remove(divisor)
	f.release(divisor)

	result := src
	if dest != regNone && dest != src {
		result = dest
		f.a.MovReg64(dest, src)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.op = opNone
	return result
}

// cmpIntMin compares the dividend in RAX against the type's most-negative value
// (INT_MIN), for the div_s overflow check. The 32-bit INT_MIN fits an imm32; the
// 64-bit one needs a scratch register (RAX/RDX/divisor are pinned here, so
// allocReg avoids them).
func (f *fn) cmpIntMin(w bool) {
	if w {
		t := f.allocReg(0)
		f.a.MovImm64(t, 0x8000000000000000)
		f.a.AluRR(0x39, RAX, t, true) // cmp rax, t
		f.release(t)
	} else {
		f.a.AluRI(7, RAX, int32(-2147483648), false) // cmp eax, INT_MIN
	}
}

// condenseInto materializes value/deferred elem e into the specific register dest
// (the target-hint / in-place path — the left spine of an accumulator writes
// straight into dest).
func (f *fn) condenseInto(e *elem, dest Reg) {
	if e.isDeferred() {
		f.condense(e, dest)
		return
	}
	switch e.st.kind {
	case stReg:
		if e.st.reg != dest {
			f.a.MovReg64(dest, e.st.reg)
			f.release(e.st.reg)
		}
	case stConst:
		f.loadConst(dest, e.st)
	case stSlot:
		f.a.Load64(dest, RSP, f.spillOff(e.st.slot))
	case stLocalRef:
		f.a.Load64(dest, RSP, f.localOff(e.st.idx))
	case stLocalReg, stGlobReg:
		if e.st.reg != dest {
			f.a.MovReg64(dest, e.st.reg) // copy from the pinned local/global; never release it
		}
	case stMemRef:
		f.loadMemRef(dest, e.st) // emit the deferred load into dest
		f.releaseMemRef(e.st)
	}
}

// applyALU emits `dest = dest <op> right`, folding the right operand: constants
// as immediates, memory-resident operands as an r/m read, registers as reg-reg.
func (f *fn) applyALU(enc aluEnc, dest Reg, right *elem, w bool) {
	switch right.st.kind {
	case stConst:
		if fitsImm32(right.st.cval) {
			f.a.AluRI(enc.digit, dest, int32(right.st.cval), w)
		} else {
			t := f.allocReg(maskOf(dest))
			f.loadConst(t, right.st)
			f.a.AluRR(enc.rr, dest, t, w)
			f.release(t)
		}
	case stReg:
		f.a.AluRR(enc.rr, dest, right.st.reg, w)
		f.release(right.st.reg)
	case stLocalReg, stGlobReg:
		f.a.AluRR(enc.rr, dest, right.st.reg, w) // pinned local/global; never release
	case stSlot:
		f.a.AluRM(enc.rm, dest, RSP, f.spillOff(right.st.slot), w)
	case stLocalRef:
		f.a.AluRM(enc.rm, dest, RSP, f.localOff(right.st.idx), w)
	case stMemRef:
		if memRefFoldable(right.st, w) {
			f.a.AluIdx(enc.rm, dest, RBX, right.st.reg, right.st.memDisp(), w) // op dest, [mem]
			f.releaseMemRef(right.st)
		} else {
			r := f.memRefValue(right.st)
			f.a.AluRR(enc.rr, dest, r, w)
			f.release(r)
			f.releaseMemRef(right.st)
		}
	}
}

// applyMul emits `dest = dest * right` (imul), folding the right operand.
func (f *fn) applyMul(dest Reg, right *elem, w bool) {
	switch right.st.kind {
	case stConst:
		// x*{3,5,9} → one-cycle LEA [x+x*{2,4,8}] (powers of two already became
		// shifts at pushBinOp).
		switch right.st.cval {
		case 3, 5, 9:
			f.a.LeaScaledW(dest, dest, dest, uint8(log2u(uint64(right.st.cval-1))), 0, w)
			return
		}
		if fitsImm32(right.st.cval) {
			f.a.ImulRI(dest, int32(right.st.cval), w)
		} else {
			t := f.allocReg(maskOf(dest))
			f.loadConst(t, right.st)
			f.a.IMul(dest, t, w)
			f.release(t)
		}
	case stReg:
		f.a.IMul(dest, right.st.reg, w)
		f.release(right.st.reg)
	case stLocalReg, stGlobReg:
		f.a.IMul(dest, right.st.reg, w) // pinned local/global; never release
	case stSlot:
		f.a.ImulRM(dest, RSP, f.spillOff(right.st.slot), w)
	case stLocalRef:
		f.a.ImulRM(dest, RSP, f.localOff(right.st.idx), w)
	case stMemRef:
		if memRefFoldable(right.st, w) {
			f.a.ImulIdx(dest, RBX, right.st.reg, right.st.memDisp(), w)
			f.releaseMemRef(right.st)
		} else {
			r := f.memRefValue(right.st)
			f.a.IMul(dest, r, w)
			f.release(r)
			f.releaseMemRef(right.st)
		}
		f.release(right.st.reg)
	}
}

// cmpRR emits a register-register compare of the correct width.
func (f *fn) cmpRR(x, y Reg, w bool) {
	if w {
		f.a.Cmp64(x, y)
	} else {
		f.a.Cmp32(x, y)
	}
}

// consumeBlockBelow unlinks every physical stack element of node's valent block
// that sits below node (its operand sub-trees), leaving node as the top.
func (f *fn) consumeBlockBelow(node *elem) {
	base := baseOfValentBlock(node)
	e := node.prev
	for {
		prev := e.prev
		isBase := e == base
		f.erase(e)
		if isBase {
			break
		}
		e = prev
	}
}

func fitsImm32(v int64) bool { return v >= -1<<31 && v < 1<<31 }
