//go:build amd64

package amd64

// valueFacts is bounded semantic provenance carried directly by a Valent node.
// It shares one metadata byte with GC/EH root state, so facts neither allocate
// nor enlarge the operand-stack arena. Facts describe the value, not its current
// register or slot, and therefore survive materialization and spills.
type valueFacts uint8

const (
	factUpper32Zero valueFacts = 1 << iota
	factBoolean
	storageGCRoot
	storageEHRoot
	valueFactMask = factUpper32Zero | factBoolean
)

func (facts valueFacts) has(want valueFacts) bool { return facts&want == want }

func (st storage) valueFacts() valueFacts { return valueFacts(st.meta) & valueFactMask }

func (st *storage) setValueFacts(facts valueFacts) {
	st.meta = st.meta&^uint8(valueFactMask) | uint8(facts&valueFactMask)
}

func (st storage) hasGCRoot() bool { return valueFacts(st.meta).has(storageGCRoot) }

func (st *storage) setGCRoot(root bool) {
	if root {
		st.meta |= uint8(storageGCRoot)
	} else {
		st.meta &^= uint8(storageGCRoot)
	}
}

func (st storage) hasEHRoot() bool { return valueFacts(st.meta).has(storageEHRoot) }

func (st *storage) setEHRoot(root bool) {
	if root {
		st.meta |= uint8(storageEHRoot)
	} else {
		st.meta &^= uint8(storageEHRoot)
	}
}

func deferredResultFacts(op wOp, typ machineType) valueFacts {
	if isCompare(op) || op == opEqz {
		return factUpper32Zero | factBoolean
	}
	if op == opWrap || op == opZExt32 {
		return factUpper32Zero
	}
	if typ != mtI32 {
		return 0
	}
	switch op {
	case opAdd, opSub, opAnd, opOr, opXor,
		opShl, opShrU, opShrS, opRotl, opRotr,
		opMul, opDivU, opDivS, opRemU, opRemS,
		opClz, opCtz, opPopcnt:
		return factUpper32Zero
	}
	return 0
}
