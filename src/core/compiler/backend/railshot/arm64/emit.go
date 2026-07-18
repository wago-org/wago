//go:build arm64

package arm64

import a64 "github.com/wago-org/wago/src/core/encoder/arm64"

// The condense engine: materialize a deferred-action valent block into machine
// code, with target hints (compute the result straight into a destination
// register and reuse operand registers in place). Ported from WARP's
// condenseValentBlock / emitDeferredAction / selectInstr operand folding.
//
// AArch64 is a load/store, three-operand, orthogonal RISC: no memory operands, no
// flag side-effects on ordinary ALU ops, and no fixed-register instructions. Every
// fold x86 did inside one instruction becomes an explicit sequence here — but the
// operand-stack / valent-block / condense architecture is unchanged; only the leaf
// lowering differs (see the arm64 CONTRACT §4).

// aluEnc identifies a binary integer ALU op for the applyALU dispatcher. On x86
// this carried the encoding bytes (rr/rm/digit) an instruction selected between;
// AArch64 has a distinct orthogonal instruction per op (ADD/SUB/AND/ORR/EOR in
// both widths), so the "encoding" reduces to the wasm op itself plus the
// commutativity flag the selector consults.
type aluEnc struct {
	op   wOp
	comm bool
}

var aluTable = map[wOp]aluEnc{
	opAdd: {opAdd, true},
	opSub: {opSub, false},
	opAnd: {opAnd, true},
	opOr:  {opOr, true},
	opXor: {opXor, true},
}

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
	panic("arm64: unsupported deferred op")
}

// condenseConvert lowers the integer width conversions (wrap / sign- & zero-
// extend). Each reads the source register and writes the converted value; the
// source register can be reused when there is no target hint.
// producesCleanI32 reports whether an i32-typed deferred op materializes into a
// register whose upper 32 bits are guaranteed zero. All of these lower to 32-bit
// instructions (ALU/shift/mul/div, bit counts) or a 0/1 cset, and a 32-bit
// (W-register) write clears the upper half on AArch64, exactly as on x86-64. Loads
// and local/global reads are excluded: they can surface dirty upper bits
// (garbage-padded params, sign-extending loads).
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

func (f *fn) condenseConvert(node *elem, dest Reg) Reg {
	// i32.wrap_i64(i64.extend_i32_{s,u}(x)) is exactly x's low 32 bits. Keep the
	// i32 carrier canonical with a W-register move, but skip the otherwise useless
	// sign/zero extension. This occurs frequently in code that widens only for an
	// intermediate ABI or arithmetic operation before returning to i32.
	roundTrip := node.op == opWrap && node.arg0.kind == ekDeferred &&
		(node.arg0.op == opZExt32 || node.arg0.op == opSExt32)
	if roundTrip {
		src := f.materialize(node.arg0.arg0)
		result := src
		if dest != regNone {
			result = dest
		}
		f.a.MovReg32(result, src)
		if result != src {
			f.release(src)
		}
		f.stats.peep("extend-wrap-elim")
		f.consumeBlockBelow(node)
		f.occupy(node, result)
		node.op = opNone
		return result
	}

	// Redundant zero-extend elimination: i64.extend_i32_u of a value already in
	// clean zero-upper form (an i32 produced by a 32-bit instruction, which zeroes
	// the upper 32 bits on AArch64) is a no-op. Captured before materialize consumes
	// the deferred node. NOT applied to i32 locals/params or sign-extending loads,
	// which can carry dirty upper bits — hence the producer-op whitelist.
	cleanZExt := node.op == opZExt32 && node.arg0.kind == ekDeferred &&
		node.arg0.typ == mtI32 && producesCleanI32(node.arg0.op)
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
		f.a.MovReg32(result, src) // 32-bit MOV (ORR Wd,WZR,Wm) zero-extends into the X register
	case opWrap:
		f.a.MovReg32(result, src)
	case opSExt32:
		f.a.Sxtw(result, src) // SBFM Xd,Xn,#0,#31 — sign-extend 32→64
	case opSExt8:
		// Encoder width selectors use true for W (32-bit) and false for X
		// (64-bit), the inverse of machineType.is64().
		f.a.Sxtb(result, src, !node.typ.is64())
	case opSExt16:
		f.a.Sxth(result, src, !node.typ.is64())
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
// place (const→imm, memory→register load, reg→reg). AArch64 is three-operand, so
// the in-place accumulate is `op Rd,Rn,Rm` with Rd==Rn==dest.
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

	// MADD/MSUB fusion: add(c, a*b) → MADD (c + a*b), sub(c, a*b) → MSUB (c - a*b)
	// in one instruction when the multiply is an un-condensed value*value node.
	// Checked before the LEA/local-sink forms so a*b±c never splits into MUL + ADD.
	if node.op == opAdd || node.op == opSub {
		if r := f.tryMulAddFuse(node, dest, w); r != regNone {
			return r
		}
	}

	// AArch64's integer ALU is genuinely three-operand.  When local.set gives
	// us a destination register and the left input is a borrowed pinned
	// local/global, use it as Rn directly instead of first copying it into Rd:
	//
	//   local.set $a (i32.add (local.get $b) (local.get $c))
	//
	// becomes ADD Wa, Wb, Wc rather than MOV Wa,Wb; ADD Wa,Wa,Wc.  This is the
	// small, lifetime-aware part of wazero's allocator that fits Railshot's
	// direct compiler: the source stays borrowed until this one instruction and
	// the destination's local lifetime begins immediately afterwards.  Restrict
	// it to concrete RHS values so we never change deferred evaluation order or
	// register-pressure behavior.
	if threeOperandSinkEnabled && dest != regNone && left.kind == ekValue &&
		(left.st.kind == stLocalReg || left.st.kind == stGlobReg) &&
		(right.kind == ekValue || (right.isDeferred() && (isUnary(right.op) || isConvert(right.op)))) {
		if f.tryThreeOperandLocalSink(node, dest, left, right, w) {
			return dest
		}
	}

	// Scaled-index fusion: add(x, shl(y, k∈1..3)) → `ADD dest, x, y, LSL #k` (the
	// add-shifted-register form) — one instruction replacing shl+add. The common
	// AssemblyScript array-address shape (`base + (i << log2size)`).
	if node.op == opAdd {
		if r := f.tryLeaScaledAdd(node, left, right, dest); r != regNone {
			return r
		}
		if r := f.tryUxtwAdd(node, left, right, dest); r != regNone {
			return r
		}
	}

	// Strength-reduce x * {3,5,9} to a single add-shifted `[x + x*{2,4,8}]` (base ==
	// index == x), replacing a MUL by a small constant. The multiplier sits on the
	// right after the commutative swap above.
	if node.op == opMul {
		if r := f.tryLeaMul(node, left, right, dest); r != regNone {
			return r
		}
	}

	// AArch64 can preserve an old local value used as the RHS while computing a
	// new LHS, then write the final three-register result straight back into that
	// same local:
	//
	//     x = complex(y) | x    =>    ...complex into t; ORR x,t,x
	//
	// The old path parked x in a spill slot because condenseInto(left, x) would
	// overwrite it. Protecting x from allocation while the LHS condenses avoids
	// both the store and reload without a scratch copy. This shape is pervasive in
	// the unrolled BLAKE compression round.
	if oldDestRHSSinkEnabled && dest != regNone && right.kind == ekValue &&
		(right.st.kind == stReg || right.st.kind == stLocalReg || right.st.kind == stGlobReg) &&
		right.st.reg == dest {
		f.pinned = f.pinned.add(dest)
		lr, owned := f.materializeRead(left)
		f.pinned = f.pinned.remove(dest)
		f.aluRR3(node.op, dest, lr, dest, w)
		if owned && lr != dest {
			f.release(lr)
		}
		f.stats.peep("old-dest-rhs-sink")
		f.consumeBlockBelow(node)
		f.occupy(node, dest)
		node.op = opNone
		return dest
	}

	// Materialize the RHS into a safe, foldable operand BEFORE the LHS overwrites
	// dest: condense a deferred RHS to a fresh register, and copy a register RHS
	// out if it aliases dest.
	rightReleaseAfter := regNone
	pinnedRight := regNone
	if right.isDeferred() {
		rr := f.condense(right, regNone)
		// Computing the LHS next can clobber the just-condensed RHS register when it
		// writes into `dest` (a RHS that landed in dest is lost). Unlike x86, AArch64
		// has NO fixed-register ALU ops (mul/div/shift are all orthogonal), so there
		// is no RAX/RDX/RCX hard-target hazard to guard against — only the dest-alias
		// hazard remains. Relocate the RHS to a scratch clear of dest and pin it
		// across the LHS.
		if dest != regNone && rr == dest {
			avoid := maskOf(dest)
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
				// instead — applyALU reloads it. `right` is the on-stack condensed
				// node, so the slot stays visible to the allocator until
				// consumeBlockBelow erases it.
				f.spill(right)
			}
		}
		// Otherwise leave `right` as the condensed on-stack node (do NOT detach into
		// a {stReg: rr} copy): computing the LHS can spill it under register pressure
		// — reached most readily via a load's inline bounds check (explicit mode),
		// whose allocReg reclaims rr — and applyALU then reloads it from its spill
		// slot. A detached copy would instead read rr after the spill freed and reused
		// it, silently reading a clobbered register (the inflate/flush_block
		// explicit-mode miscompile: the i64 bit-buffer OR). The on-stack node tracks
		// the spill; consumeBlockBelow erases it and applyALU releases its register.
		// Mirrors the spill-fallback in the relocate branch above.
	} else if (right.st.kind == stReg || right.st.kind == stLocalReg || right.st.kind == stGlobReg) && dest != regNone && right.st.reg == dest {
		// In-place self-update (e.g. `x = (a<<b) | x`): the old RHS lives in dest,
		// which computing the LHS will overwrite. Spill it to a slot so applyALU
		// reloads it from memory. Copying it into a scratch register and computing the
		// LHS through that scratch instead risks the scratch being reused under
		// register pressure (an unpinned copy → the guard-page miscompile), while
		// pinning the copy perturbs the allocator's register choices for unrelated
		// values (a two-consumer value desynced → a quicksort miscompile). Spilling
		// touches no register, so it is safe on both counts. (The inflate/flush_block
		// guard-page miscompile: `L11 = (…) | L11`.)
		f.spill(right)
	}

	if dest == regNone {
		// selectInstr forms (choose the cheapest emission):
		//  - LEA-style add:  `ADD dst, local, reg|#imm` computes local+x in one insn
		//    without clobbering the pinned local (which a two-source add accumulating
		//    in place would require a preceding copy for).
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

// tryThreeOperandLocalSink emits a binary operation directly into a pinned
// local/global destination when its left input is another borrowed pinned value.
// A pure unary RHS is condensed to one owned scratch first, allowing patterns
// such as `a = b - clz(a)` to become `CLZ tmp,a; SUB a,b,tmp` without the
// otherwise-required `MOV a,b`. More general deferred trees retain the ordinary,
// fully pressure-aware condenseBinary path.
func (f *fn) tryThreeOperandLocalSink(node *elem, dest Reg, left, right *elem, w bool) bool {
	var rhs Reg
	ownedRHS := false
	if right.isDeferred() {
		if !isUnary(right.op) && !isConvert(right.op) {
			return false
		}
		// A 32-bit consumer needs only the low half of
		// wrap(extend_i32_{s,u}(x)); when x is a borrowed pin, feed it directly
		// to the W-form ALU. That W write canonicalizes the result for free.
		if !w && right.op == opWrap && right.arg0 != nil && right.arg0.isDeferred() &&
			(right.arg0.op == opZExt32 || right.arg0.op == opSExt32) && right.arg0.arg0 != nil &&
			right.arg0.arg0.kind == ekValue &&
			(right.arg0.arg0.st.kind == stLocalReg || right.arg0.arg0.st.kind == stGlobReg) {
			rhs = right.arg0.arg0.st.reg
			f.stats.peep("extend-wrap-elim")
		} else {
			// dest and the borrowed left source are pinned local/global registers, so
			// condenseUnary's allocator cannot select either. It realizes the old RHS
			// value before the destination local's lifetime is overwritten.
			rhs = f.condense(right, regNone)
			ownedRHS = true
		}
	} else {
		switch right.st.kind {
		case stConst:
			if f.aluImm3(node.op, dest, left.st.reg, right.st.cval, w) {
				f.stats.peep("local-3op-sink")
				f.consumeBlockBelow(node)
				f.occupy(node, dest)
				node.op = opNone
				return true
			}
			// MUL has no immediate encoding, and a non-encodable ALU immediate needs
			// one short-lived scratch.  The source local is still not copied.
			rhs = f.allocReg(maskOf(dest, left.st.reg))
			f.loadConst(rhs, right.st)
			ownedRHS = true
		case stReg:
			rhs, ownedRHS = right.st.reg, true
		case stLocalReg, stGlobReg:
			rhs = right.st.reg
		default:
			return false
		}
	}

	f.aluRR3(node.op, dest, left.st.reg, rhs, w)
	if ownedRHS {
		f.release(rhs)
	}
	f.stats.peep("local-3op-sink")
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return true
}

// shlByConst123 reports whether e is a deferred shl of node-typ t by a constant
// masked count in 1..3 (an add-shifted-encodable scale), returning the count.
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
// add-shifted-register `ADD dest, x, y, LSL #k`. Returns the result register, or
// regNone when the shape doesn't match.
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
	// values: condensing a deferred operand here could clobber the other operand's
	// register under pressure. The AS address shape (local/global base + local
	// index) is concrete by the time the add is built.
	if other.kind != ekValue || shl.arg0 == nil || shl.arg0.kind != ekValue {
		return regNone
	}
	// The index: read a pinned local in place (add-shifted never writes its
	// sources); anything else materializes into an owned register.
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
	f.leaScaled(dest, x, y, uint8(k), 0, w)
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

// tryUxtwAdd lowers i64.add(x, i64.extend_i32_u(y)) to a single extended-register
// add `ADD Xdest, Xx, Wy, UXTW`, folding the zero-extend into the add. UXTW reads
// only y's low 32 bits (zero-extended), which is exactly extend_i32_u's value, so
// y's upper bits are irrelevant — no separate zero-extend is needed. Both operands
// must be concrete: condensing a deferred operand here could clobber the other's
// register under pressure (the same hazard tryLeaScaledAdd guards against). Only
// the 64-bit add matches (extend_i32_u produces i64).
func (f *fn) tryUxtwAdd(node, left, right *elem, dest Reg) Reg {
	if !uxtwAddEnabled || !node.typ.is64() {
		return regNone
	}
	ext, other := right, left
	if !isZExt32Deferred(ext) {
		ext, other = left, right
		if !isZExt32Deferred(ext) {
			return regNone
		}
	}
	if other.kind != ekValue || ext.arg0 == nil || ext.arg0.kind != ekValue {
		return regNone
	}
	// Read the extend source's low 32 bits in place when it's a pinned local; the
	// extended-register add never writes its sources. Pin it across the other
	// operand's materialization.
	y, yOwned := f.materializeRead(ext.arg0)
	f.pinned = f.pinned.add(y)
	x, xOwned := f.materializeRead(other)
	f.pinned = f.pinned.remove(y)
	if dest == regNone {
		switch {
		case xOwned:
			dest = x
		case yOwned:
			dest = y
		default:
			dest = f.allocReg(0)
		}
	}
	f.stats.peep("uxtw-add")
	f.a.AddExtUXTW(dest, x, y)
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

func isZExt32Deferred(e *elem) bool {
	return e != nil && e.kind == ekDeferred && e.op == opZExt32
}

// tryLeaMul lowers x * {3,5,9} as a single add-shifted `dest = x + x*{2,4,8}` (base
// == index == x), replacing a MUL by a small constant. Returns regNone when the
// shape doesn't match. The multiplicand must be concrete: condensing a deferred
// operand here could clobber a register under the add-shifted (same hazard as
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
	x, xOwned := f.materializeRead(left) // add-shifted never writes its sources; a pinned local reads in place
	if dest == regNone {
		if xOwned {
			dest = x // reuse the owned multiplicand in place
		} else {
			dest = f.allocReg(0)
		}
	}
	f.leaScaled(dest, x, x, scaleLog, 0, w)
	if xOwned && x != dest {
		f.release(x)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return dest
}

// leaRightOK reports whether the right add operand can be an add-shifted index /
// immediate displacement.
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

// emitLeaAdd emits `dst = base + right` without writing base (a register-resident
// value that must be preserved): a constant folds via add/sub-immediate (or a
// materialized register for large displacements), a register via add-shifted with
// scale 0 (a plain reg-reg ADD). Releases an owned register right.
func (f *fn) emitLeaAdd(dst, base Reg, right *elem, w bool) {
	switch right.st.kind {
	case stConst:
		f.leaDisp(dst, base, int32(right.st.cval), w)
	case stReg:
		f.leaScaled(dst, base, right.st.reg, 0, 0, w)
		f.release(right.st.reg)
	case stLocalReg, stGlobReg:
		f.leaScaled(dst, base, right.st.reg, 0, 0, w) // pinned local/global; never released
	}
}

// leaScaled lowers x86's `lea dst,[base + idx<<scale + disp]` to AArch64 arithmetic:
// ADD dst, base, idx, LSL #scale (the add-shifted-register form), then fold the
// displacement. There is no arm64 LEA instruction; this is its faithful expansion.
func (f *fn) leaScaled(dst, base, idx Reg, scale uint8, disp int32, w bool) {
	// The backend helpers use w==true for 64-bit operations; the encoder's
	// AddShifted flag selects the 32-bit W-form when true.
	f.a.AddShifted(dst, base, idx, scale, !w) // ADD dst, base, idx, LSL #scale
	if disp != 0 {
		f.addDisp(dst, dst, disp, w)
	}
}

// leaDisp lowers `lea dst,[base + disp]` to add/sub-immediate (or a copy when disp
// is zero).
func (f *fn) leaDisp(dst, base Reg, disp int32, w bool) {
	if disp == 0 {
		if dst != base {
			f.a.MovReg64(dst, base)
		}
		return
	}
	f.addDisp(dst, base, disp, w)
}

// addDisp emits `dst = base + disp` using the 12-bit add/sub-immediate form when
// the magnitude fits, else materializing the displacement in the backend scratch
// X16 and using the register form.
func (f *fn) addDisp(dst, base Reg, disp int32, w bool) {
	switch {
	case disp >= 0 && disp <= 0xFFF:
		if w {
			f.a.AddImm64(dst, base, uint32(disp))
		} else {
			f.a.AddImm32(dst, base, uint32(disp))
		}
	case disp < 0 && -disp <= 0xFFF:
		if w {
			f.a.SubImm64(dst, base, uint32(-disp))
		} else {
			f.a.SubImm32(dst, base, uint32(-disp))
		}
	default:
		if w {
			f.a.MovImm64(X16, uint64(int64(disp)))
			f.a.Add64(dst, base, X16)
		} else {
			f.a.MovImm64(X16, uint64(uint32(disp)))
			f.a.Add32(dst, base, X16)
		}
	}
}

// condenseShift lowers shl/shr_s/shr_u/rotl/rotr. A constant count folds to an
// immediate shift; a variable count uses AArch64's orthogonal LSLV/LSRV/ASRV/RORV
// (shift any register by any register, taken mod width) — so none of x86's "force
// the count into CL / spill RCX / pin RCX" dance is needed.
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
		f.shiftImm(digit, dest, byte(right.st.cval&mask), w)
		f.pinned = f.pinned.remove(dest)
		f.consumeBlockBelow(node)
		f.occupy(node, dest)
		node.op = opNone
		return dest
	}

	// Variable count. AArch64's shift-by-register ops are fully orthogonal, so any
	// register can hold the value and any register the count — no fixed-register
	// scratch avoidance is needed (unlike x86's RAX/RDX/RCX constraints). A pinned
	// local self-update can use its destination directly, matching condenseBinary's
	// in-place local-set path. Keep the scratch path when the count is that same
	// register: wasm requires the old count after the value destination is written.
	// Evaluate left before right (wasm order).
	val := regNone
	if dest != regNone && left.kind == ekValue && left.st.kind == stLocalReg && left.st.reg == dest &&
		!(right.kind == ekValue && right.st.kind == stLocalReg && right.st.reg == dest) {
		val = dest
	}
	if val == regNone {
		val = f.allocReg(0)
	}
	f.pinned = f.pinned.add(val)
	f.condenseInto(left, val)
	cnt := f.materialize(right)
	f.pinned = f.pinned.add(cnt)
	f.shiftVar(digit, val, cnt, w)
	f.pinned = f.pinned.remove(cnt)
	f.release(cnt)
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

// shiftImm emits a constant-count shift/rotate. rotl has no direct arm64 op:
// rotl(x,c) = ror(x, width-c), so it lowers to RorImm over the complemented count.
func (f *fn) shiftImm(k shiftKind, dst Reg, cnt byte, w bool) {
	ew := !w // encoder shift helpers use true for the 32-bit W-form.
	switch k {
	case shLSL:
		f.a.LslImm(dst, dst, cnt, ew)
	case shLSR:
		f.a.LsrImm(dst, dst, cnt, ew)
	case shASR:
		f.a.AsrImm(dst, dst, cnt, ew)
	case shROR:
		f.a.RorImm(dst, dst, cnt, ew)
	case shROL:
		width := byte(32)
		if w {
			width = 64
		}
		f.a.RorImm(dst, dst, (width-cnt)&(width-1), ew) // rotl(x,c) = ror(x, width-c)
	}
}

// shiftVar emits a variable-count shift/rotate via LSLV/LSRV/ASRV/RORV. rotl has
// no arm64 ROL: rotl(x,n) = rorv(x, -n) — RORV reduces its count mod width, and
// -n mod width is the left-rotate amount. The negation uses the backend scratch X16.
func (f *fn) shiftVar(k shiftKind, dst, cnt Reg, w bool) {
	switch k {
	case shLSL:
		f.lslv(dst, dst, cnt, w)
	case shLSR:
		f.lsrv(dst, dst, cnt, w)
	case shASR:
		f.asrv(dst, dst, cnt, w)
	case shROR:
		f.rorv(dst, dst, cnt, w)
	case shROL:
		if w {
			f.a.Sub64(X16, ZR, cnt)
		} else {
			f.a.Sub32(X16, ZR, cnt)
		}
		f.rorv(dst, dst, X16, w)
	}
}

func (f *fn) lslv(d, n, m Reg, w bool) {
	if w {
		f.a.Lslv64(d, n, m)
	} else {
		f.a.Lslv32(d, n, m)
	}
}
func (f *fn) lsrv(d, n, m Reg, w bool) {
	if w {
		f.a.Lsrv64(d, n, m)
	} else {
		f.a.Lsrv32(d, n, m)
	}
}
func (f *fn) asrv(d, n, m Reg, w bool) {
	if w {
		f.a.Asrv64(d, n, m)
	} else {
		f.a.Asrv32(d, n, m)
	}
}
func (f *fn) rorv(d, n, m Reg, w bool) {
	if w {
		f.a.Rorv64(d, n, m)
	} else {
		f.a.Rorv32(d, n, m)
	}
}

// condenseCompare lowers the relational ops and eqz to a CMP (or CMN for a
// negative immediate) + Cset, producing a 0/1 i32 result. (Fusing compares
// directly into branches is a later optimization; Phase 1 materializes the
// boolean.) AArch64 has no memory operands, so a slot/local-ref/mem-ref right
// operand is loaded into a register and compared register-register.
func (f *fn) condenseCompare(node *elem, dest Reg) Reg {
	if node.typ.isFloat() { // deferred ordered float compare materialized as a value
		return f.condenseFCompareValue(node, dest)
	}
	if cc, ok := f.tryMaskedEqzToFlags(node); ok {
		result := dest
		if result == regNone {
			result = f.allocReg(0)
		}
		f.stats.peep("compare-setcc")
		f.a.Cset32(result, cc)
		f.occupy(node, result)
		node.st.typ = mtI32
		node.op = opNone
		return result
	}
	w := node.typ.is64()
	left := node.arg0

	// cmp reads the left comparand read-only, so a borrowed pinned-local/global
	// register can feed the compare in place — no copy — provided nothing between
	// the read and the compare writes it. That holds for eqz and for a constant
	// right operand (the compare is emitted immediately, the only intervening emit
	// being a const load into a fresh temp). For any other right operand we keep the
	// copy: a deferred right could be condensed here and clobber, and comparing the
	// live pinned register afterwards would read a post-write value. When L is a
	// borrowed register the trailing cset must not clobber it, so the boolean lands
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
		f.cmpImm(L, 0, w) // SUBS XZR, L, #0
	} else {
		cc = condOf(node.op)
		right := node.arg1
		if right.isDeferred() {
			rr := f.condense(right, regNone)
			right = &elem{kind: ekValue, st: storage{kind: stReg, typ: node.typ, reg: rr}}
		}
		switch right.st.kind {
		case stConst:
			if fitsAddSubImm12(right.st.cval) {
				f.cmpImmS(L, right.st.cval, w)
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
			t := f.allocReg(maskOf(L))
			f.ld64(t, SP, f.spillOff(right.st.slot))
			f.cmpRR(L, t, w)
			f.release(t)
		case stLocalRef:
			t := f.allocReg(maskOf(L))
			f.ld64(t, SP, f.localOff(right.st.idx))
			f.cmpRR(L, t, w)
			f.release(t)
		case stMemRef:
			// arm64: a deferred load is never foldable into a CMP (memRefFoldable is
			// always false), so materialize it and compare register-register.
			f.loadMemRef(right.st.reg, right.st)
			f.cmpRR(L, right.st.reg, w)
			f.release(right.st.reg)
		}
	}
	f.pinned = f.pinned.remove(L)

	// Choose the register the cset boolean lands in. If we own L (a scratch temp),
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
	f.a.Cset32(result, cc) // relational result is a 0/1 i32
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.st.typ = mtI32 // relational result is always i32
	node.op = opNone
	return result
}

// condenseUnary lowers clz/ctz/popcnt (CLZ; RBIT+CLZ; NEON popcount).
func (f *fn) condenseUnary(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	// clz/ctz/popcnt read their source read-only, so a register-resident source
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
		f.a.Clz(result, src, !w)
	case opCtz:
		// AArch64 has no CTZ: reverse the bits, then count leading zeros.
		f.a.Rbit(result, src, !w)
		f.a.Clz(result, result, !w)
	case opPopcnt:
		// AArch64 base has no scalar POPCNT. Use a normal V-register scratch so
		// mixed int/float code observes the same spill/avoid rules as scalar FP.
		vPop := f.allocFReg(0)
		f.a.FmovFromGpr(vPop, src, w) // fmov d/s, x/w
		f.a.Cnt8b(vPop, vPop)         // per-byte popcount
		f.a.Addv8b(vPop, vPop)        // horizontal sum of the byte lanes
		f.a.FmovToGpr(result, vPop, false)
		f.releaseF(vPop)
	}
	if srcOwned && result != src {
		f.release(src)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.op = opNone
	return result
}

// condenseDivRem lowers div_s/div_u/rem_s/rem_u using AArch64's orthogonal
// SDIV/UDIV (any registers, no RDX:RAX pair, no Cdq sign-extend) and MSUB for the
// remainder (rem = dividend - quot*divisor), with the two wasm-mandated integer
// division traps: divide-by-zero (all four ops) and the signed INT_MIN/-1 overflow
// (div_s only; rem_s must instead yield 0 without faulting). Because there are no
// fixed division registers, x86's spill-RAX/spill-RDX/pin/Cdq dance disappears —
// three ordinary registers (dividend, divisor, result) suffice.
func (f *fn) condenseDivRem(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	signed := node.op == opDivS || node.op == opRemS
	wantRem := node.op == opRemS || node.op == opRemU
	left := node.arg0
	right := node.arg1

	// Constant divisor: strength-reduce to shifts / multiply-high, avoiding the divide.
	if right.kind == ekValue && right.st.kind == stConst {
		if r, ok := f.tryDivByConst(node, dest, right.st.cval); ok {
			return r
		}
	}

	// Divisor and dividend into ordinary registers (SDIV/UDIV read both without
	// clobbering either).
	divisor := f.materialize(right)
	f.pinned = f.pinned.add(divisor)
	dividend := f.allocReg(maskOf(divisor))
	f.pinned = f.pinned.add(dividend)
	f.condenseInto(left, dividend)

	// Divide-by-zero trap for every division op.
	f.cmpImm(divisor, 0, w)
	f.trapIf(condE, trapDivZero)

	// The result register: honor the caller hint when it is free of the operand
	// registers, else a fresh temp (the remainder path also needs a scratch quotient
	// distinct from result).
	result := dest
	if result == regNone || result == divisor || result == dividend {
		result = f.allocReg(maskOf(divisor, dividend))
	}

	switch {
	case signed && !wantRem: // div_s: INT_MIN / -1 would fault — trap it as overflow
		f.cmpImmS(divisor, -1, w) // cmp divisor, -1
		noOvf := f.a.Bcond(condNE)
		f.cmpIntMin(dividend, w) // cmp dividend, INT_MIN
		f.trapIf(condE, trapDivOverflow)
		f.a.PatchBranch19(noOvf, f.a.Len())
		f.sdiv(result, dividend, divisor, w)
	case signed: // rem_s: x % -1 == 0, computed directly to avoid the INT_MIN/-1 fault
		f.cmpImmS(divisor, -1, w) // cmp divisor, -1
		notM1 := f.a.Bcond(condNE)
		f.a.MovImm64(result, 0) // remainder is 0
		done := f.a.Branch()
		f.a.PatchBranch19(notM1, f.a.Len())
		q := f.allocReg(maskOf(divisor, dividend, result))
		f.sdiv(q, dividend, divisor, w)
		f.msub(result, q, divisor, dividend, w) // rem = dividend - q*divisor
		f.release(q)
		f.a.PatchBranch26(done, f.a.Len())
	case !wantRem: // div_u
		f.udiv(result, dividend, divisor, w)
	default: // rem_u
		q := f.allocReg(maskOf(divisor, dividend, result))
		f.udiv(q, dividend, divisor, w)
		f.msub(result, q, divisor, dividend, w) // rem = dividend - q*divisor
		f.release(q)
	}

	f.pinned = f.pinned.remove(divisor)
	f.pinned = f.pinned.remove(dividend)
	f.release(divisor)
	f.release(dividend)

	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.op = opNone
	return result
}

func (f *fn) sdiv(d, n, m Reg, w bool) {
	if w {
		f.a.Sdiv64(d, n, m)
	} else {
		f.a.Sdiv32(d, n, m)
	}
}
func (f *fn) udiv(d, n, m Reg, w bool) {
	if w {
		f.a.Udiv64(d, n, m)
	} else {
		f.a.Udiv32(d, n, m)
	}
}

// msub emits `d = ra - n*m` (MSUB), the remainder computation dividend - quot*divisor.
func (f *fn) msub(d, n, m, ra Reg, w bool) {
	if w {
		f.a.Msub64(d, n, m, ra)
	} else {
		f.a.Msub32(d, n, m, ra)
	}
}

// madd emits `d = ra + n*m` (MADD), the fused multiply-add.
func (f *fn) madd(d, n, m, ra Reg, w bool) {
	if w {
		f.a.Madd64(d, n, m, ra)
	} else {
		f.a.Madd32(d, n, m, ra)
	}
}

// isValueMul reports whether e is a deferred integer multiply whose two operands
// are both concrete values (not nested deferred subtrees). Such a mul can fuse
// into a single MADD/MSUB with a value addend without any nested-subtree consume.
func isValueMul(e *elem) bool {
	return e != nil && e.kind == ekDeferred && e.op == opMul &&
		e.arg0 != nil && e.arg0.kind == ekValue &&
		e.arg1 != nil && e.arg1.kind == ekValue
}

// tryMulAddFuse fuses add(c, a*b) → MADD (d = c + a*b) and sub(c, a*b) → MSUB
// (d = c - a*b) into one instruction when the multiply is an un-condensed opMul
// node with value operands and the addend is a value. a*b - c is NOT MSUB-shaped
// (MSUB computes ra - n*m), so only the c-minus-mul sub form fuses. Returns the
// result register or regNone when the shape does not apply. Gated by
// WAGO_NO_MULADD as the A/B oracle.
func (f *fn) tryMulAddFuse(node *elem, dest Reg, w bool) Reg {
	if !mulAddFuseEnabled {
		return regNone
	}
	var mul, addend *elem
	switch node.op {
	case opAdd:
		switch {
		case isValueMul(node.arg1):
			mul, addend = node.arg1, node.arg0
		case isValueMul(node.arg0):
			mul, addend = node.arg0, node.arg1
		}
	case opSub:
		if isValueMul(node.arg1) { // c - a*b → MSUB; a*b - c is not representable
			mul, addend = node.arg1, node.arg0
		}
	}
	if mul == nil || addend.kind != ekValue {
		return regNone
	}
	// Three read-only sources; pin each so materializing the next (e.g. a load or
	// const) cannot reuse it.
	n, ownN := f.materializeRead(mul.arg0)
	f.pinned = f.pinned.add(n)
	m, ownM := f.materializeRead(mul.arg1)
	f.pinned = f.pinned.add(m)
	ra, ownRa := f.materializeRead(addend)
	f.pinned = f.pinned.add(ra)
	d := dest
	if d == regNone {
		d = f.allocReg(0)
	}
	if node.op == opAdd {
		f.madd(d, n, m, ra, w)
	} else {
		f.msub(d, n, m, ra, w)
	}
	f.pinned = f.pinned.remove(n)
	f.pinned = f.pinned.remove(m)
	f.pinned = f.pinned.remove(ra)
	if ownN {
		f.release(n)
	}
	if ownM && m != n {
		f.release(m)
	}
	if ownRa && ra != n && ra != m {
		f.release(ra)
	}
	f.stats.peep("mul-add-fuse")
	f.consumeBlockBelow(node)
	f.occupy(node, d)
	node.op = opNone
	return d
}

// cmpIntMin compares the dividend against the type's most-negative value
// (INT_MIN), for the div_s overflow check. AArch64 has no large compare immediate,
// so INT_MIN is materialized in the backend scratch X16 (all value registers are
// live here) and compared register-register.
func (f *fn) cmpIntMin(dividend Reg, w bool) {
	if w {
		f.a.MovImm64(X16, 0x8000000000000000)
		f.a.CmpReg64(dividend, X16)
	} else {
		f.a.MovImm64(X16, uint64(uint32(0x80000000))) // 32-bit INT_MIN
		f.a.CmpReg32(dividend, X16)
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
		f.ld64(dest, SP, f.spillOff(e.st.slot))
	case stLocalRef:
		f.ld64(dest, SP, f.localOff(e.st.idx))
	case stLocalReg, stGlobReg:
		if e.st.reg != dest {
			f.a.MovReg64(dest, e.st.reg) // copy from the pinned local/global; never release it
		}
	case stMemRef:
		f.loadMemRef(dest, e.st) // emit the deferred load into dest
		f.releaseMemRef(e.st)
	}
}

// applyALU emits `dest = dest <op> right`, folding the right operand: constants as
// immediates (add/sub 12-bit, or logical bitmask-immediate) when encodable, else
// materialized into a register; memory-resident operands are loaded first (no
// AArch64 memory operands); registers are used directly.
func (f *fn) applyALU(enc aluEnc, dest Reg, right *elem, w bool) {
	switch right.st.kind {
	case stConst:
		if !f.aluImm(enc.op, dest, right.st.cval, w) {
			t := f.allocReg(maskOf(dest))
			f.loadConst(t, right.st)
			f.aluRR(enc.op, dest, t, w)
			f.release(t)
		}
	case stReg:
		f.aluRR(enc.op, dest, right.st.reg, w)
		f.release(right.st.reg)
	case stLocalReg, stGlobReg:
		f.aluRR(enc.op, dest, right.st.reg, w) // pinned local/global; never release
	case stSlot:
		t := f.allocReg(maskOf(dest))
		f.ld64(t, SP, f.spillOff(right.st.slot))
		f.aluRR(enc.op, dest, t, w)
		f.release(t)
	case stLocalRef:
		t := f.allocReg(maskOf(dest))
		f.ld64(t, SP, f.localOff(right.st.idx))
		f.aluRR(enc.op, dest, t, w)
		f.release(t)
	case stMemRef:
		// arm64: no memory-operand ALU (memRefFoldable is always false) — load then reg-reg.
		r := f.memRefValue(right.st)
		f.aluRR(enc.op, dest, r, w)
		f.release(r)
		f.releaseMemRef(right.st)
	}
}

// aluRR emits the in-place reg-reg ALU op `dest = dest <op> src` (Rd==Rn==dest).
func (f *fn) aluRR(op wOp, dest, src Reg, w bool) {
	f.aluRR3(op, dest, dest, src, w)
}

// aluRR3 emits the three-register form `dest = left <op> right`.  Unlike the
// in-place convenience wrapper above, it preserves left; this is what lets a
// local.set sink consume a borrowed pinned local without a copy.
func (f *fn) aluRR3(op wOp, dest, left, right Reg, w bool) {
	switch op {
	case opAdd:
		if w {
			f.a.Add64(dest, left, right)
		} else {
			f.a.Add32(dest, left, right)
		}
	case opSub:
		if w {
			f.a.Sub64(dest, left, right)
		} else {
			f.a.Sub32(dest, left, right)
		}
	case opAnd:
		if w {
			f.a.And64(dest, left, right)
		} else {
			f.a.And32(dest, left, right)
		}
	case opOr:
		if w {
			f.a.Orr64(dest, left, right)
		} else {
			f.a.Orr32(dest, left, right)
		}
	case opXor:
		if w {
			f.a.Eor64(dest, left, right)
		} else {
			f.a.Eor32(dest, left, right)
		}
	case opMul:
		if w {
			f.a.Mul64(dest, left, right)
		} else {
			f.a.Mul32(dest, left, right)
		}
	}
}

// aluImm tries to fold a constant right operand as an AArch64 immediate: add/sub
// use the 12-bit add/sub-immediate form (with a negative value folding to the
// other op), the logical ops use the bitmask-immediate encoding (the encoder's
// AndImm/OrrImm/EorImm return ok=false when the constant is not a valid rotated run
// of ones). Returns false when no immediate form encodes the constant, so the
// caller materializes it in a register and uses the reg-reg form (replacing x86's
// single fitsImm32 gate).
func (f *fn) aluImm(op wOp, dest Reg, cval int64, w bool) bool {
	return f.aluImm3(op, dest, dest, cval, w)
}

// aluImm3 is aluImm with an independent left input. It is used for the same
// local-set sink as aluRR3, so immediate arithmetic retains the one-instruction
// form rather than first copying the source local into the destination.
func (f *fn) aluImm3(op wOp, dest, left Reg, cval int64, w bool) bool {
	switch op {
	case opAdd:
		return f.addFoldImm3(dest, left, cval, w)
	case opSub:
		return f.addFoldImm3(dest, left, -cval, w) // left - cval == left + (-cval)
	case opAnd:
		if w {
			return f.a.AndImm64(dest, left, uint64(cval))
		}
		return f.a.AndImm32(dest, left, uint32(cval))
	case opOr:
		if w {
			return f.a.OrrImm64(dest, left, uint64(cval))
		}
		return f.a.OrrImm32(dest, left, uint32(cval))
	case opXor:
		if w {
			return f.a.EorImm64(dest, left, uint64(cval))
		}
		return f.a.EorImm32(dest, left, uint32(cval))
	}
	return false
}

// addFoldImm folds `dest = dest + v` (signed) into the 12-bit add/sub-immediate
// form: a non-negative v uses AddImm, a negative v uses SubImm of its magnitude.
// Returns false when |v| exceeds the 12-bit range.
func (f *fn) addFoldImm(dest Reg, v int64, w bool) bool {
	return f.addFoldImm3(dest, dest, v, w)
}

// addFoldImm3 emits `dest = base + v` when v fits AArch64's add/sub-immediate
// encoding. The in-place addFoldImm wrapper retains existing callers.
func (f *fn) addFoldImm3(dest, base Reg, v int64, w bool) bool {
	switch {
	case v >= 0 && v <= 0xFFF:
		if w {
			f.a.AddImm64(dest, base, uint32(v))
		} else {
			f.a.AddImm32(dest, base, uint32(v))
		}
		return true
	case v < 0 && -v <= 0xFFF:
		if w {
			f.a.SubImm64(dest, base, uint32(-v))
		} else {
			f.a.SubImm32(dest, base, uint32(-v))
		}
		return true
	}
	return false
}

// applyMul emits `dest = dest * right`, folding the right operand. AArch64 has no
// multiply-immediate, so a constant is either the {3,5,9} add-shifted special case
// or materialized into a register; memory operands are loaded first.
func (f *fn) applyMul(dest Reg, right *elem, w bool) {
	switch right.st.kind {
	case stConst:
		// x*{3,5,9} → one add-shifted [x+x*{2,4,8}] (powers of two already became
		// shifts at pushBinOp).
		switch right.st.cval {
		case 3, 5, 9:
			f.leaScaled(dest, dest, dest, uint8(log2u(uint64(right.st.cval-1))), 0, w)
			return
		}
		t := f.allocReg(maskOf(dest))
		f.loadConst(t, right.st)
		f.mulRR(dest, t, w)
		f.release(t)
	case stReg:
		f.mulRR(dest, right.st.reg, w)
		f.release(right.st.reg)
	case stLocalReg, stGlobReg:
		f.mulRR(dest, right.st.reg, w) // pinned local/global; never release
	case stSlot:
		t := f.allocReg(maskOf(dest))
		f.ld64(t, SP, f.spillOff(right.st.slot))
		f.mulRR(dest, t, w)
		f.release(t)
	case stLocalRef:
		t := f.allocReg(maskOf(dest))
		f.ld64(t, SP, f.localOff(right.st.idx))
		f.mulRR(dest, t, w)
		f.release(t)
	case stMemRef:
		// arm64: no memory-operand MUL (memRefFoldable is always false) — load then reg-reg.
		r := f.memRefValue(right.st)
		f.mulRR(dest, r, w)
		f.release(r)
		f.releaseMemRef(right.st)
	}
}

// mulRR emits the in-place low-half multiply `dest = dest * src` (MADD …,XZR).
func (f *fn) mulRR(dest, src Reg, w bool) {
	if w {
		f.a.Mul64(dest, dest, src)
	} else {
		f.a.Mul32(dest, dest, src)
	}
}

// cmpRR emits a register-register compare of the correct width (SUBS XZR,x,y).
func (f *fn) cmpRR(x, y Reg, w bool) {
	if w {
		f.a.CmpReg64(x, y)
	} else {
		f.a.CmpReg32(x, y)
	}
}

// cmpImm emits a compare against a small unsigned 12-bit immediate (SUBS XZR,x,#imm).
func (f *fn) cmpImm(x Reg, imm uint32, w bool) {
	if w {
		f.a.CmpImm64(x, imm)
	} else {
		f.a.CmpImm32(x, imm)
	}
}

// cmpImmS emits a compare against a signed 12-bit immediate: a negative value uses
// CMN (compare-negative, ADDS XZR,x,#|v|) so the flags match `cmp x,#v`. The caller
// must have gated on fitsAddSubImm12.
func (f *fn) cmpImmS(x Reg, cval int64, w bool) {
	if cval < 0 {
		if w {
			f.a.CmnImm64(x, uint32(-cval))
		} else {
			f.a.CmnImm32(x, uint32(-cval))
		}
		return
	}
	f.cmpImm(x, uint32(cval), w)
}

// fitsAddSubImm12 reports whether v fits the 12-bit add/sub (compare) immediate in
// either sign — the AArch64 replacement for the amd64 fitsImm32 compare gate.
func fitsAddSubImm12(v int64) bool { return v >= -0xFFF && v <= 0xFFF }

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

// memRefFoldable reports whether a deferred load may be folded directly as an
// ALU/CMP operand. AArch64 has no memory operands, so a deferred load can NEVER be
// folded — applyALU/applyMul/condenseCompare therefore always take their
// materialize→reg-reg path (see the arm64 CONTRACT §4a).
func memRefFoldable(st storage, w bool) bool { return false }

func fitsImm32(v int64) bool { return v >= -1<<31 && v < 1<<31 }

var _ = a64.X0 // ensure the a64 import is referenced even before the encoder methods land
