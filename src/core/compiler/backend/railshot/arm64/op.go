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

// Operation categories: each deferred op is condensed by one of the emit paths
// (binary-ALU, shift, compare, unary, div/rem), dispatched from condense().

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
