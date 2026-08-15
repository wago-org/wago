//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type valueFacts = shared.ValueFacts

const (
	factUpper32Zero = shared.ValueFactUpper32Zero
	factBoolean     = shared.ValueFactBoolean
	factNonZero     = shared.ValueFactNonZero
	factSignExt8    = shared.ValueFactSignExt8
	factSignExt16   = shared.ValueFactSignExt16
	factSignExt32   = shared.ValueFactSignExt32
)

func (f *fn) factsForLocal(x int) valueFacts {
	if f.localFactsEnabled && uint(x) < uint(len(f.locals)) {
		return f.locals[x].facts
	}
	return 0
}

func (f *fn) setFactsForLocal(x int, facts valueFacts) {
	if f.localFactsEnabled && uint(x) < uint(len(f.locals)) {
		f.locals[x].facts = facts
	}
}

func (f *fn) applyFactsForLocal(e *elem, x int) {
	facts := f.factsForLocal(x)
	e.st.facts = facts
	if facts != 0 {
		f.stats.peep("local-fact")
	}
}

func (f *fn) applyFactsForTypedResult(e *elem, typ wasm.ValType) {
	if e != nil && typ.Kind() == wasm.ValRef && !typ.Ref().Nullable() && f.opt(optValueFacts) {
		e.st.facts |= factNonZero
	}
}

func (f *fn) applyFactsForTypedResults(results []wasm.ValType) {
	if !f.opt(optValueFacts) || len(results) == 0 {
		return
	}
	e := f.s.back()
	for i := len(results) - 1; i >= 0 && e != nil && e != f.s.head; i-- {
		f.applyFactsForTypedResult(e, results[i])
		e = baseOfValentBlock(e).prev
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
	switch op {
	case opSExt8:
		facts := factSignExt8 | factSignExt16
		if typ == mtI64 {
			facts |= factSignExt32
		}
		return facts
	case opSExt16:
		facts := valueFacts(factSignExt16)
		if typ == mtI64 {
			facts |= factSignExt32
		}
		return facts
	case opSExt32:
		return factSignExt32
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

func redundantSignExtension(op wOp, resultType, inputType machineType, facts valueFacts) bool {
	if resultType != inputType {
		return false
	}
	switch op {
	case opSExt8:
		return facts.Has(factSignExt8)
	case opSExt16:
		return facts.Has(factSignExt16)
	case opSExt32:
		return facts.Has(factSignExt32)
	}
	return false
}
