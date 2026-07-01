package x64

// wOp is the operation a deferred-action node performs — the backend's internal
// opcode, lowered from a wasm opcode by the driver. Ported from the operation
// taxonomy WARP threads through emitDeferredAction (warp x86_64_backend.cpp);
// kept as a flat Go enum. Extended per phase; Phase 0 needs only the basic ALU
// ops, with the rest reserved for later phases.
type wOp uint8

const (
	opNone wOp = iota

	// Integer binary ALU (commutativity/encoding handled by selectInstr).
	opAdd
	opSub
	opAnd
	opOr
	opXor
	opShl
	opShrU
	opShrS
	opRotl
	opRotr
	opMul
	opDivU
	opDivS
	opRemU
	opRemS

	// Integer unary.
	opClz
	opCtz
	opPopcnt

	// Comparisons (produce a flag/bool).
	opEq
	opNe
	opLtS
	opLtU
	opGtS
	opGtU
	opLeS
	opLeU
	opGeS
	opGeU
	opEqz

	// Memory loads (sideEffect set on the node).
	opLoad
)

// aluOps is the set of straight two-operand integer ALU ops selectInstr can fold
// operands for. commutativity is per-op.
func (o wOp) commutative() bool {
	switch o {
	case opAdd, opAnd, opOr, opXor, opMul, opEq, opNe:
		return true
	}
	return false
}
