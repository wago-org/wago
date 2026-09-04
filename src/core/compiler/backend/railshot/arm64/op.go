//go:build arm64

package arm64

// wOp is the operation a deferred-action node performs — the backend's internal
// opcode, lowered from a wasm opcode by the driver. Ported from the operation
// taxonomy WARP threads through emitDeferredAction (warp aarch64 backend); kept
// as a flat Go enum. Extended per phase; Phase 0 needs only the basic ALU ops,
// with the rest reserved for later phases.
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
	opSExt32 // i64 <- i32 sign-extend (sxtw); also i64.extend32_s
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

// shiftKind enumerates the arm64 variable/immediate shift-and-rotate lowerings a
// shift op condenses to. AArch64 has no x86-style ModRM /digit selecting a shift
// variant inside one instruction; instead each op maps to an orthogonal
// instruction (LSLV/LSRV/ASRV for the shifts, RORV for the rotates). This
// replaces the amd64 backend's `/digit` selector (which returned the shl/shr/sar/
// rol/ror ModRM extension). rotl has no direct arm64 op and is realized by the
// shift emit path as RORV over the negated (width-complemented) count — see
// condenseShift — so it maps to the same rotate kind and the emit path handles
// the count fixup.
type shiftKind uint8

const (
	shLSL shiftKind = iota // logical shift left  (LSLV / LSL #imm)
	shLSR                  // logical shift right (LSRV / LSR #imm)
	shASR                  // arithmetic shift right (ASRV / ASR #imm)
	shROL                  // rotate left  (RORV over width-count; no direct op)
	shROR                  // rotate right (RORV / ROR #imm)
)

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
