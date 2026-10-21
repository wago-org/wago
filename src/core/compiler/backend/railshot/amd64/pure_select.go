//go:build amd64

package amd64

import "os"

// Branches can regress high-entropy conditions. Keep this measured workload
// option explicit until the compiler has a reliable branch-probability proof.
var pureSelectEnabled = os.Getenv("WAGO_AMD64_PURE_SELECT") == "1"

const pureSelectMaxOps = 12

// pureSelectChain is a bounded left spine. Every right operand is a captured
// integer value. This shape needs only the result and one scratch register.
type pureSelectChain struct {
	nodes [pureSelectMaxOps]*elem
	leaf  *elem
	n     int
	cost  int
	muls  int
}

func pureSelectLeaf(e *elem, typ machineType) bool {
	if e == nil || !e.isValue() || e.st.typ != typ || e.st.hasGCRoot() {
		return false
	}
	switch e.st.kind {
	case stConst, stReg, stSlot, stLocalReg:
		return true
	}
	return false
}

func provePureSelectChain(e *elem, typ machineType) (p pureSelectChain, ok bool) {
	for e != nil && e.isDeferred() {
		if p.n == len(p.nodes) || e.valueType() != typ || e.st.hasGCRoot() ||
			!isBinALU(e.deferredOp()) || !pureSelectLeaf(e.arg1, typ) {
			return p, false
		}
		p.nodes[p.n] = e
		p.n++
		p.cost++
		if e.deferredOp() == opMul {
			p.cost += 2
			p.muls++
		}
		e = e.arg0
	}
	p.leaf = e
	return p, pureSelectLeaf(e, typ) && p.muls != 0 && p.cost >= 4
}

// tryPureExpensiveSelect skips only unobservable pending arithmetic. The caller
// must first materializeTrapsBefore the condition, as in ordinary emitSelect.
// local.tee has already committed its assignment in setLocal. Frame loads here
// read captured operand spills, never linear memory or a mutable local home.
func (f *fn) tryPureExpensiveSelect() bool {
	if !pureSelectEnabled {
		return false
	}
	cond := f.s.back()
	if cond == nil {
		return false
	}
	b := baseOfValentBlock(cond).prev
	if b == f.s.head {
		return false
	}
	a := baseOfValentBlock(b).prev
	if a == f.s.head {
		return false
	}
	typ := rootMachineType(a)
	if (typ != mtI32 && typ != mtI64) || rootMachineType(b) != typ {
		return false
	}
	costly, cheap, skipCC := a, b, condE
	p, ok := provePureSelectChain(costly, typ)
	if !ok || !pureSelectLeaf(cheap, typ) {
		costly, cheap, skipCC = b, a, condNE
		p, ok = provePureSelectChain(costly, typ)
		if !ok || !pureSelectLeaf(cheap, typ) {
			return false
		}
	}
	// Reject obvious pressure before touching the condition. In particular, a
	// deferred compare must remain deferred on this fallback so emitSelect can
	// still fuse its flags. The later check covers fixed-register displacement.
	conditionNeed := 1
	if cond.isValue() && cond.st.kind == stReg {
		conditionNeed = 0
	} else if cond.isDeferred() {
		conditionNeed = int(cond.registerNeed())
		if conditionNeed == 0 {
			conditionNeed = 1
		}
	}
	if _, _, free := f.pureSelectFreeRegisters(&p, cheap, regNone); free < 2+conditionNeed {
		return false
	}

	// Condition lowering is eager and can displace captured registers (including
	// x86 fixed registers). Reprove the tree after it completes. A rejection here
	// leaves a valid materialized condition for the ordinary select fallback.
	creg := f.materialize(cond)
	p, ok = provePureSelectChain(costly, typ)
	if !ok || !pureSelectLeaf(cheap, typ) {
		return false
	}
	result, scratch, _ := f.pureSelectFreeRegisters(&p, cheap, creg)
	if scratch == regNone {
		return false // No spills, local eviction, or conditional state changes.
	}

	f.readPureSelectLeaf(result, cheap)
	f.a.TestSelf(creg, false)
	skip := f.a.JccPlaceholder(skipCC)
	f.readPureSelectLeaf(result, p.leaf)
	w := typ == mtI64
	for i := p.n - 1; i >= 0; i-- {
		node, right := p.nodes[i], p.nodes[i].arg1
		if regallocCheckEnabled {
			f.checkUse(right)
		}
		if right.st.kind == stConst && (!w || fitsImm32(right.st.cval)) {
			if node.deferredOp() == opMul {
				f.a.ImulRI(result, int32(right.st.cval), w)
			} else {
				f.a.AluRI(aluTable[node.deferredOp()].digit, result, int32(right.st.cval), w)
			}
		} else {
			rreg := right.st.reg
			if right.st.kind != stReg && right.st.kind != stLocalReg {
				f.readPureSelectLeaf(scratch, right)
				rreg = scratch
			}
			if node.deferredOp() == opMul {
				f.a.IMul(result, rreg, w)
			} else {
				f.a.AluRR(aluTable[node.deferredOp()].rr, result, rreg, w)
			}
		}
	}
	f.a.PatchRel32(skip, f.a.Len())

	// No allocator work occurred between the branch and join. Only the result
	// differs across the two paths. Release consumed owners after both emissions.
	f.release(creg)
	if cheap.st.kind == stReg {
		f.release(cheap.st.reg)
	}
	if p.leaf.st.kind == stReg {
		f.release(p.leaf.st.reg)
	}
	for i := 0; i < p.n; i++ {
		if right := p.nodes[i].arg1; right.st.kind == stReg {
			f.release(right.st.reg)
		}
	}
	f.consumeBlockBelow(costly)
	f.erase(cond)
	f.erase(cheap)
	f.occupy(costly, result)
	f.stats.peep("pure-select-branch")
	return true
}

func (f *fn) pureSelectFreeRegisters(p *pureSelectChain, cheap *elem, cond Reg) (result, scratch Reg, free int) {
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	if cond != regNone {
		block = block.add(cond)
	}
	for _, leaf := range []*elem{p.leaf, cheap} {
		if leaf.st.kind == stReg || leaf.st.kind == stLocalReg {
			block = block.add(leaf.st.reg)
		}
	}
	for i := 0; i < p.n; i++ {
		right := p.nodes[i].arg1
		if right.st.kind == stReg || right.st.kind == stLocalReg {
			block = block.add(right.st.reg)
		}
	}
	result, scratch = regNone, regNone
	for _, r := range gpAlloc {
		if !block.has(r) && f.regUser[r] == nil {
			if free == 0 {
				result = r
			} else if free == 1 {
				scratch = r
			}
			free++
		}
	}
	return
}

func (f *fn) readPureSelectLeaf(dst Reg, e *elem) {
	if regallocCheckEnabled {
		f.checkUse(e)
	}
	switch e.st.kind {
	case stConst:
		f.loadConst(dst, e.st)
	case stReg, stLocalReg:
		f.moveInt(dst, e.st.reg, e.st.typ)
	case stSlot:
		f.loadFrameInt(dst, f.spillOff(e.st.slotIndex()), e.st.typ)
	}
}
