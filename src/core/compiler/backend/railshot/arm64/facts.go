//go:build arm64

package arm64

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

func (f *fn) factsForLocal(x int) valueFacts {
	// A serialized i32 parameter can arrive in a 64-bit carrier with high bits
	// set. Declared i32 locals have a stronger representation invariant: their
	// zero initializer is canonical, and setLocal canonicalizes any assignment
	// whose producer does not already write a W register. That invariant survives
	// control joins without assignment-version sidecars.
	if uint(x) >= uint(len(f.locals)) {
		return 0
	}
	facts := valueFacts(0)
	if f.canonicalI32Local(x) {
		facts |= factUpper32Zero
	}
	if f.localFactsEnabled {
		facts |= f.locals[x].facts
	}
	return facts
}

func (f *fn) declaredI32Local(x int) bool {
	return f.opt(optValueFacts) && x >= f.nParams && uint(x) < uint(len(f.localType)) && f.localType[x] == mtI32
}

func (f *fn) canonicalI32Local(x int) bool {
	return f.declaredI32Local(x) || uint(x) < 64 && f.canonicalI32Params&(uint64(1)<<uint(x)) != 0
}

func (f *fn) canonicalizeI32LocalAssignment(x int, reg Reg, facts valueFacts) {
	if f.canonicalI32Local(x) && !facts.has(factUpper32Zero) {
		f.a.MovReg32(reg, reg)
		f.stats.peep("local-i32-canonicalize")
	}
}

func canonicalI32ParamMask(hints *funcHintView, localTypes []machineType, nParams int, enabled bool) uint64 {
	if !enabled || !hints.flags.has(hintTouchesMemory) {
		return 0
	}
	var mask uint64
	for i := 0; i < min(nParams, min(len(localTypes), min(len(hints.localScore), 64))); i++ {
		// One entry canonicalization replaces at least two direct scalar-load
		// address canonicalizations. Single-use parameters retain per-use lowering.
		if localTypes[i] == mtI32 && hints.localScore[i]&localScoreParamAddressReuse != 0 {
			mask |= uint64(1) << uint(i)
		}
	}
	return mask
}

func (f *fn) setFactsForLocal(x int, facts valueFacts) {
	if f.localFactsEnabled && uint(x) < uint(len(f.locals)) {
		f.locals[x].facts = facts
	}
}

func (f *fn) applyFactsForLocal(e *elem, x int) {
	facts := f.factsForLocal(x)
	e.st.setValueFacts(facts)
	if f.localFactsEnabled && uint(x) < uint(len(f.locals)) && f.locals[x].facts != 0 {
		f.stats.peep("local-fact")
	}
}

// deferredResultFacts returns only properties guaranteed by the Wasm operation
// and ARM64's W-register write semantics. Reads from locals, globals, memory, or
// unknown calls begin with no facts and cannot enter through this function.
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
