//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

type valueFacts = shared.ValueFacts

const (
	factUpper32Zero = shared.ValueFactUpper32Zero
	factBoolean     = shared.ValueFactBoolean
	factSignExt8    = shared.ValueFactSignExt8
	factSignExt16   = shared.ValueFactSignExt16
	factSignExt32   = shared.ValueFactSignExt32
)

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
