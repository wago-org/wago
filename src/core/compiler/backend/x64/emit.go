package x64

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
	panic("x64: unsupported deferred op")
}

// condenseConvert lowers the integer width conversions (wrap / sign- & zero-
// extend). Each reads the source register and writes the converted value; the
// source register can be reused when there is no target hint.
func (f *fn) condenseConvert(node *elem, dest Reg) Reg {
	src := f.materialize(node.arg0)
	result := src
	if dest != regNone && dest != src {
		result = dest
	}
	switch node.op {
	case opWrap, opZExt32:
		// 32-bit mov zero-extends into the full 64-bit register.
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

	// Materialize the RHS into a safe, foldable operand BEFORE the LHS overwrites
	// dest: condense a deferred RHS to a fresh register, and copy a register RHS
	// out if it aliases dest.
	rightReleaseAfter := regNone
	if right.isDeferred() {
		rr := f.condense(right, regNone)
		right = &elem{kind: ekValue, st: storage{kind: stReg, typ: node.typ, reg: rr}}
		rightReleaseAfter = rr
	} else if right.st.kind == stReg && dest != regNone && right.st.reg == dest {
		t := f.allocReg(maskOf(dest))
		f.a.MovReg64(t, dest)
		right = &elem{kind: ekValue, st: storage{kind: stReg, typ: node.typ, reg: t}}
		rightReleaseAfter = t
	}

	if dest == regNone {
		// selectInstr forms (choose the cheapest emission):
		//  - LEA add:  `lea dst, [local + reg|imm]` computes local+x in one insn
		//    without clobbering the pinned local (which reg-reg add would require a
		//    preceding copy for).
		//  - in-place: reuse an owned-register left as the destination, so the op
		//    accumulates in place with no preceding mov.
		if node.op == opAdd && left.kind == ekValue && left.st.kind == stLocalReg && leaRightOK(right) {
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
	f.release(rightReleaseAfter)

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
	case stReg, stLocalReg:
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
	case stLocalReg:
		f.a.LeaScaledW(dst, base, right.st.reg, 0, 0, w) // pinned local; never released
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

	// Variable count → CL. Evaluate the count, move it into RCX (spilling RCX's
	// occupant), then compute the shifted value into a dest register other than RCX.
	cnt := f.materialize(right)
	if cnt != RCX {
		f.spillIfUsed(RCX)
		f.a.MovReg64(RCX, cnt)
		f.release(cnt)
	}
	f.pinned = f.pinned.add(RCX)
	if dest == regNone || dest == RCX {
		dest = f.allocReg(maskOf(RCX))
	}
	f.pinned = f.pinned.add(dest)
	f.condenseInto(left, dest)
	f.a.ShiftCL(digit, dest, w)
	f.pinned = f.pinned.remove(dest)
	f.pinned = f.pinned.remove(RCX)
	f.release(RCX)
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return dest
}

// condenseCompare lowers the relational ops and eqz to a CMP/TEST + SETcc,
// producing a 0/1 i32 result. (Fusing compares directly into branches is a later
// optimization; Phase 1 materializes the boolean.)
func (f *fn) condenseCompare(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	left := node.arg0

	L := f.materialize(left)
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
		case stLocalReg:
			f.cmpRR(L, right.st.reg, w) // pinned local; never release
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

	result := L
	if dest != regNone && dest != L {
		result = dest
		f.release(L)
	}
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
	if arg.kind == ekValue && arg.st.kind == stLocalReg {
		src, srcOwned = arg.st.reg, false // pinned local: read directly, never release
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
// (quotient) / RDX (remainder) registers. (Divide-by-zero and INT_MIN/-1 trap
// checks are added with the trap runtime in Phase 3.)
func (f *fn) condenseDivRem(node *elem, dest Reg) Reg {
	w := node.typ.is64()
	signed := node.op == opDivS || node.op == opRemS
	wantRem := node.op == opRemS || node.op == opRemU
	left := node.arg0
	right := node.arg1

	// Reserve RAX (dividend/quotient) and RDX (high half/remainder).
	f.spillIfUsed(RAX)
	f.spillIfUsed(RDX)
	f.pinned = f.pinned.add(RAX)
	f.pinned = f.pinned.add(RDX)

	// Divisor into any non-RAX/RDX register.
	divisor := f.materialize(right)
	f.pinned = f.pinned.add(divisor)

	// Dividend into RAX, then sign/zero-extend into RDX.
	f.condenseInto(left, RAX)
	if signed {
		f.a.Cdq(w) // sign-extend RAX → RDX:RAX
		f.a.Idiv(divisor, w)
	} else {
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
	case stLocalReg:
		if e.st.reg != dest {
			f.a.MovReg64(dest, e.st.reg) // copy from the pinned local; never release it
		}
	case stMemRef:
		f.loadMemRef(dest, e.st) // emit the deferred load into dest
		f.release(e.st.reg)
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
	case stLocalReg:
		f.a.AluRR(enc.rr, dest, right.st.reg, w) // pinned local; never release
	case stSlot:
		f.a.AluRM(enc.rm, dest, RSP, f.spillOff(right.st.slot), w)
	case stLocalRef:
		f.a.AluRM(enc.rm, dest, RSP, f.localOff(right.st.idx), w)
	case stMemRef:
		if memRefFoldable(right.st, w) {
			f.a.AluIdx(enc.rm, dest, RBX, right.st.reg, right.st.memDisp(), w) // op dest, [mem]
		} else {
			f.loadMemRef(right.st.reg, right.st)
			f.a.AluRR(enc.rr, dest, right.st.reg, w)
		}
		f.release(right.st.reg)
	}
}

// applyMul emits `dest = dest * right` (imul), folding the right operand.
func (f *fn) applyMul(dest Reg, right *elem, w bool) {
	switch right.st.kind {
	case stConst:
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
	case stLocalReg:
		f.a.IMul(dest, right.st.reg, w) // pinned local; never release
	case stSlot:
		f.a.ImulRM(dest, RSP, f.spillOff(right.st.slot), w)
	case stLocalRef:
		f.a.ImulRM(dest, RSP, f.localOff(right.st.idx), w)
	case stMemRef:
		if memRefFoldable(right.st, w) {
			f.a.ImulIdx(dest, RBX, right.st.reg, right.st.memDisp(), w)
		} else {
			f.loadMemRef(right.st.reg, right.st)
			f.a.IMul(dest, right.st.reg, w)
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
		f.s.erase(e)
		if isBase {
			break
		}
		e = prev
	}
}

func fitsImm32(v int64) bool { return v >= -1<<31 && v < 1<<31 }
