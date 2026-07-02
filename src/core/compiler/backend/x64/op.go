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

	// Integer width conversions (unary).
	opWrap   // i32 <- i64 (truncate low 32, zero upper)
	opSExt32 // i64 <- i32 sign-extend (movsxd); also i64.extend32_s
	opZExt32 // i64 <- i32 zero-extend
	opSExt8  // sign-extend low 8  (width from typ)
	opSExt16 // sign-extend low 16 (width from typ)

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

// Operation categories: each deferred op is condensed by one of the emit paths
// (binary-ALU, shift, compare, unary, div/rem), dispatched from condense().

func isBinALU(o wOp) bool {
	switch o {
	case opAdd, opSub, opAnd, opOr, opXor, opMul:
		return true
	}
	return false
}

func isShift(o wOp) bool {
	switch o {
	case opShl, opShrU, opShrS, opRotl, opRotr:
		return true
	}
	return false
}

func isCompare(o wOp) bool {
	switch o {
	case opEq, opNe, opLtS, opLtU, opGtS, opGtU, opLeS, opLeU, opGeS, opGeU:
		return true
	}
	return false
}

func isUnary(o wOp) bool {
	switch o {
	case opClz, opCtz, opPopcnt:
		return true
	}
	return false
}

func isConvert(o wOp) bool {
	switch o {
	case opWrap, opSExt32, opZExt32, opSExt8, opSExt16:
		return true
	}
	return false
}

func isDivRem(o wOp) bool {
	switch o {
	case opDivU, opDivS, opRemU, opRemS:
		return true
	}
	return false
}

// shiftDigit is the /digit ModRM extension selecting the x86 shift/rotate variant.
func shiftDigit(o wOp) byte {
	switch o {
	case opShl:
		return 4 // shl
	case opShrU:
		return 5 // shr (logical)
	case opShrS:
		return 7 // sar (arithmetic)
	case opRotl:
		return 0 // rol
	case opRotr:
		return 1 // ror
	}
	panic("x64: not a shift op")
}

// condOf maps a compare op to its x86 condition code.
func condOf(o wOp) Cond {
	switch o {
	case opEq:
		return condE
	case opNe:
		return condNE
	case opLtS:
		return condL
	case opLtU:
		return condB
	case opGtS:
		return condG
	case opGtU:
		return condA
	case opLeS:
		return condLE
	case opLeU:
		return condBE
	case opGeS:
		return condGE
	case opGeU:
		return condAE
	}
	panic("x64: not a compare op")
}
