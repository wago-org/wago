//go:build arm64

package arm64

// aluOps is the set of straight two-operand integer ALU ops selectInstr can fold
// operands for. commutativity is per-op.
func (o wOp) commutative() bool {
	switch o {
	case opAdd, opAnd, opOr, opXor, opMul, opEq, opNe:
		return true
	}
	return false
}

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

// shiftDigit maps a shift/rotate op to its arm64 shift-instruction kind. Named to
// mirror the amd64 backend's shiftDigit (which returned the x86 ModRM /digit);
// here it returns the orthogonal arm64 shift selector the emit path lowers with
// LSLV/LSRV/ASRV/RORV. The rotl→RORV(width-count) and const-count→immediate-form
// fixups live in condenseShift, exactly as the amd64 twin kept encoding choices
// out of this pure classifier.
func shiftDigit(o wOp) shiftKind {
	switch o {
	case opShl:
		return shLSL // logical shift left
	case opShrU:
		return shLSR // logical shift right
	case opShrS:
		return shASR // arithmetic shift right
	case opRotl:
		return shROL // rotate left (RORV over width-count)
	case opRotr:
		return shROR // rotate right (RORV)
	}
	panic("arm64: not a shift op")
}

// condOf maps a compare op to its arm64 condition code. The condXX names are the
// package-local aliases for the a64 condition set (declared in cc.go), so this
// ports verbatim from the amd64 twin — only the underlying encodings changed.
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
	panic("arm64: not a compare op")
}
