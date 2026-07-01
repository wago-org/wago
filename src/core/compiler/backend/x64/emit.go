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

// condense emits the deferred node's value into a register. dest is a target
// register the result must land in (target hint, e.g. a local's register) or
// regNone to pick a fresh one. Returns the register now holding the value and
// converts `node` into that value on the stack (its operands are consumed).
func (f *fn) condense(node *elem, dest Reg) Reg {
	enc, ok := aluTable[node.op]
	if !ok {
		panic("x64: unsupported deferred op in Phase 0")
	}
	w := node.typ.is64()
	left := node.arg0
	right := node.arg1

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
		dest = f.allocReg(0)
	}
	f.pinned = f.pinned.add(dest)
	f.condenseInto(left, dest)
	f.applyALU(enc, dest, right, w)
	f.pinned = f.pinned.remove(dest)
	f.release(rightReleaseAfter)

	// Consume the whole valent block below `node` and make `node` the result.
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	node.op = opNone
	return dest
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
		f.a.Load64(dest, RBP, f.spillOff(e.st.slot))
	case stLocalRef:
		f.a.Load64(dest, RBP, f.localOff(e.st.idx))
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
	case stSlot:
		f.a.AluRM(enc.rm, dest, RBP, f.spillOff(right.st.slot), w)
	case stLocalRef:
		f.a.AluRM(enc.rm, dest, RBP, f.localOff(right.st.idx), w)
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
