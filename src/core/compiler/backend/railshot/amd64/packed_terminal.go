//go:build amd64

package amd64

import "os"

var packedTerminalEnabled = os.Getenv("WAGO_EXPERIMENTAL_PACKED_TERMINAL") == "1"

const packedTerminalKind storageKind = deferredStorageKind + 1
const (
	terminalConstant = uint32(iota)
	terminalLocalRef
	terminalLocalReg
)

func (e *elem) isPackedTerminal() bool { return e != nil && e.st.kind == packedTerminalKind }
func terminalLeafKind(e *elem) (uint32, bool) {
	if e == nil || !e.isValue() || e.st.cold != 0 || e.st.meta != uint8(storageLogicalRoot) || e.st.slot != 0 {
		return 0, false
	}
	switch e.st.kind {
	case stConst:
		return terminalConstant, true
	case stLocalRef:
		if e.st.cval == 0 {
			return terminalLocalRef, true
		}
	case stLocalReg:
		if e.st.cval == 0 && e.st.reg < 16 {
			return terminalLocalReg, true
		}
	}
	return 0, false
}
func (f *fn) tryPackTerminal(op wOp, typ machineType, left, right *elem) bool {
	if !packedTerminalEnabled || profileEnabled || regallocCheckEnabled || len(f.intervalReg) != 0 || f.inlineDepth != 0 || f.pinRelinquished || len(f.customInstructions) != 0 || (typ != mtI32 && typ != mtI64) {
		return false
	}
	if op != opAdd && op != opSub && op != opAnd && op != opOr && op != opXor {
		return false
	}
	lk, ok := terminalLeafKind(left)
	if !ok {
		return false
	}
	rk, ok := terminalLeafKind(right)
	if !ok {
		return false
	}
	if left.st.typ != typ || right.st.typ != typ || left.next != right || right.next != f.s.head || lk == terminalConstant && rk == terminalConstant {
		return false
	}
	if typ == mtI64 && (lk == terminalConstant && !fitsImm32(left.st.cval) || rk == terminalConstant && !fitsImm32(right.st.cval)) {
		return false
	}
	payload := int64(right.st.idx)
	idx := left.st.idx
	if lk == terminalConstant {
		payload = left.st.cval
		idx = right.st.idx
	} else if rk == terminalConstant {
		payload = right.st.cval
	}
	regs := Reg(uint8(left.st.reg)&15 | (uint8(right.st.reg)&15)<<4)
	f.s.erase(right)
	f.initPackedTerminal(left, op, typ, payload, idx, regs, lk, rk)
	return true
}

// The producer and legacy-leaf conversion share the exact scalar encoding.
// No temporary child nodes or generic operand getters are involved.
func (f *fn) initPackedTerminal(n *elem, op wOp, typ machineType, payload int64, idx uint32, regs Reg, lk, rk uint32) {
	n.st = storage{kind: packedTerminalKind, typ: typ, cval: payload, idx: idx, reg: regs, slot: uint32(op) | 1<<8 | 2<<16 | lk<<24 | rk<<26, meta: uint8(storageLogicalRoot)}
	if f.opt(optValueFacts) {
		n.st.setValueFacts(deferredResultFacts(op, typ))
	}
	n.arg0, n.arg1 = nil, nil
	f.s.packedCount++
	f.stats.peep("packed-terminal")
}

// Expansion is cold compatibility, not the materializer's operand API.
func (f *fn) expandPackedTerminal(n *elem) {
	if !n.isPackedTerminal() {
		return
	}
	lk, rk := (n.st.slot>>24)&3, (n.st.slot>>26)&3
	left, right := storage{typ: n.st.typ}, storage{typ: n.st.typ}
	decode := func(s *storage, k uint32, reg Reg, index uint32) {
		switch k {
		case terminalConstant:
			s.kind = stConst
			s.cval = n.st.cval
		case terminalLocalRef:
			s.kind = stLocalRef
			s.idx = index
		case terminalLocalReg:
			s.kind = stLocalReg
			s.idx = index
			s.reg = reg
		default:
			panic("invalid packed terminal kind")
		}
	}
	li, ri := n.st.idx, uint32(n.st.cval)
	if lk == terminalConstant {
		ri = n.st.idx
	}
	decode(&left, lk, Reg(uint8(n.st.reg)&15), li)
	decode(&right, rk, Reg(uint8(n.st.reg)>>4), ri)
	a := f.s.tryAlloc()
	if a == nil {
		a = f.s.allocNextChunk()
	}
	b := f.s.tryAlloc()
	if b == nil {
		b = f.s.allocNextChunk()
	}
	a.st, b.st = left, right
	prev := n.prev
	prev.next = a
	a.prev = prev
	a.next = b
	b.prev = a
	b.next = n
	n.prev = b
	n.arg0, n.arg1 = a, b
	f.s.packedCount--
	n.st.kind = deferredStorageKind
	n.st.cval = 0
	n.st.idx = 0
	n.st.reg = 0
	n.st.slot &= 0x00ffffff
	f.s.canonicalSlots = false
	f.stats.peep("packed-terminal-expand")
}

func (n *elem) packedLocalMatches(lo, hi int) bool {
	lk, rk := (n.st.slot>>24)&3, (n.st.slot>>26)&3
	li, ri := int(n.st.idx), int(uint32(n.st.cval))
	if lk == terminalConstant {
		ri = int(n.st.idx)
	}
	return lk != terminalConstant && li >= lo && li < hi || rk != terminalConstant && ri >= lo && ri < hi
}
func (f *fn) expandAllPacked() {
	if f.s.packedCount == 0 {
		return
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.isPackedTerminal() {
			f.expandPackedTerminal(e)
		}
	}
}
func packedOpcodeKeepsRepresentation(op byte) bool {
	switch op {
	case 0x20, 0x41, 0x42, 0x21, 0x22, 0x1a, 0x6a, 0x6b, 0x71, 0x72, 0x73, 0x7c, 0x7d, 0x83, 0x84, 0x85:
		return true
	}
	return false
}

func (f *fn) packedOpcodeBoundary(op byte) {
	if packedOpcodeKeepsRepresentation(op) {
		return
	}
	f.notePackedExpansion("packed-expand-opcode", f.s.packedCount)
	f.expandAllPacked()
}
func (f *fn) loadTerminalInput(kind uint32, index uint32, reg Reg, constant int64, typ machineType, dest Reg) {
	switch kind {
	case terminalConstant:
		f.loadConst(dest, storage{kind: stConst, typ: typ, cval: constant})
	case terminalLocalRef:
		f.loadFrameInt(dest, f.localAddr(int(index)), typ)
	case terminalLocalReg:
		if dest != reg {
			f.moveInt(dest, reg, typ)
		}
	}
}
func (f *fn) applyTerminalInput(op wOp, kind uint32, index uint32, reg Reg, constant int64, dest Reg, w bool) {
	enc := aluTable[op]
	switch kind {
	case terminalLocalRef:
		f.a.AluRM(enc.rm, dest, RSP, f.localAddr(int(index)), w)
	case terminalLocalReg:
		f.a.AluRR(enc.rr, dest, reg, w)
	case terminalConstant:
		if f.policy.CompactNative && (op == opAdd || op == opSub) && (constant == 1 || constant == -1) {
			if op == opAdd && constant == 1 || op == opSub && constant == -1 {
				f.a.Inc(dest, w)
			} else {
				f.a.Dec(dest, w)
			}
			f.stats.peep("inc-dec")
		} else if f.opt(optI64Mask32) && w && op == opAnd && uint64(constant) <= 0xffffffff {
			f.a.AluRI(enc.digit, dest, int32(uint32(constant)), false)
			f.stats.peep("i64-mask32")
		} else {
			f.a.AluRI(enc.digit, dest, int32(constant), w)
		}
	}
}

// No synthetic elem, operand getter object, or independent temporary owner.
func (f *fn) condensePackedTerminal(n *elem, dest Reg) Reg {
	op, typ := n.deferredOp(), n.st.typ
	w := typ.is64()
	lk, rk := (n.st.slot>>24)&3, (n.st.slot>>26)&3
	lr, rr := Reg(uint8(n.st.reg)&15), Reg(uint8(n.st.reg)>>4)
	li, ri := n.st.idx, uint32(n.st.cval)
	constant := n.st.cval
	if lk == terminalConstant {
		ri = n.st.idx
	}
	// The legacy noncommutative/disabled-commute path spills an aliased RHS.
	// Restore its independent owning node before any emission or mask mutation.
	fixed := dest == RAX || dest == RCX || dest == RDX
	commute := op.commutative() && dest != regNone && (!fixed || f.opt(optCommuteFixedSelfUpdate)) && rk == terminalLocalReg && rr == dest
	if rk == terminalLocalReg && dest != regNone && rr == dest && (!commute || !f.opt(optCommuteSelfUpdate)) {
		f.notePackedExpansion("packed-expand-rhs-alias", 1)
		f.expandPackedTerminal(n)
		return f.condenseBinary(n, dest)
	}
	if op == opSub && dest == regNone && lk == terminalConstant && constant == 0 {
		dest = f.allocReg(0)
		f.loadTerminalInput(rk, ri, rr, constant, typ, dest)
		f.a.Neg(dest, w)
		f.stats.peep("negate-zero-sub")
	} else {
		if op.commutative() && lk == terminalConstant {
			lk, rk = rk, lk
			li, ri = ri, li
			lr, rr = rr, lr
		}
		commute = op.commutative() && dest != regNone && (!fixed || f.opt(optCommuteFixedSelfUpdate)) && rk == terminalLocalReg && rr == dest
		// Two identical pinned reads are the existing affine leaf cover.
		if op == opAdd && lk == terminalLocalReg && rk == terminalLocalReg && lr == rr && f.opt(optAssocTree) {
			if dest == regNone {
				dest = f.allocReg(maskOf(lr))
			}
			f.a.LeaScaledW(dest, lr, lr, 0, 0, w)
			f.stats.peep("assoc-affine-add")
		} else if commute {
			f.stats.peep("commute-self-update-candidate")
			f.pinned = f.pinned.add(dest)
			f.applyTerminalInput(op, lk, li, lr, constant, dest, w)
			f.pinned = f.pinned.remove(dest)
			f.stats.peep("commute-self-update")
			if fixed {
				f.stats.peep("commute-fixed-self-update")
			}
		} else if dest == regNone && op == opAdd && lk == terminalLocalReg && (rk == terminalLocalReg || rk == terminalConstant && fitsImm32(constant)) {
			dest = f.allocReg(0)
			if rk == terminalConstant {
				f.a.LeaDispW(dest, lr, int32(constant), w)
			} else {
				f.a.LeaScaledW(dest, lr, rr, 0, 0, w)
			}
		} else {
			if dest == regNone {
				dest = f.allocReg(0)
			}
			f.pinned = f.pinned.add(dest)
			f.loadTerminalInput(lk, li, lr, constant, typ, dest)
			f.applyTerminalInput(op, rk, ri, rr, constant, dest, w)
			f.pinned = f.pinned.remove(dest)
		}
	}
	f.occupy(n, dest)
	n.setDeferredOp(opNone)
	f.stats.peep("packed-terminal-direct")
	return dest
}

func (f *fn) notePackedExpansion(reason string, n uint32) {
	if diagnosticsEnabled {
		for i := uint32(0); i < n; i++ {
			f.stats.peep(reason)
		}
	}
}

// Plain stable integer reads append a borrowed leaf without changing any
// existing local value or register owner. Richer local paths retain the old
// physical representation before their lookahead or pressure machinery runs.
func (f *fn) packedLocalGetBoundary(x uint32) {
	if f.s.packedCount == 0 {
		return
	}
	safe := f.inlineDepth == 0 && f.inlineForwardN == 0 && f.inlineTailConstants == nil &&
		len(f.intervalReg) == 0 && !f.pinRelinquished && int(x) < len(f.localType) &&
		(f.localType[x] == mtI32 || f.localType[x] == mtI64) && !f.gcFrameLocal(int(x))
	if safe {
		if _, _, pinned := f.pinReg(int(x)); pinned {
			state := f.locals[x].state
			safe = state == lsReg || state == lsStackReg
		}
	}
	if !safe {
		f.notePackedExpansion("packed-expand-local-read", f.s.packedCount)
		f.expandAllPacked()
	}
}

// Unlike the general boundary predicate, a binary successor necessarily
// consumes the just-created top root and expands it before matching.
func packedSuccessorMayConsumeDirectly(op byte) bool {
	switch op {
	case 0x20, 0x41, 0x42, 0x21, 0x22, 0x1a:
		return true
	}
	return false
}
