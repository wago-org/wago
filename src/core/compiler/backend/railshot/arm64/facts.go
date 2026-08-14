//go:build arm64

package arm64

// valueFacts is bounded semantic provenance carried directly by a Valent node.
// It occupies existing padding in storage, so facts neither allocate nor enlarge
// the operand-stack arena. Facts describe the value, not its current register or
// slot, and therefore survive materialization and spills.
type valueFacts uint8

const (
	factUpper32Zero valueFacts = 1 << iota
	factBoolean
)

func (facts valueFacts) has(want valueFacts) bool { return facts&want == want }

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
