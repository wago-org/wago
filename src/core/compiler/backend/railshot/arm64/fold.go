//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"math/bits"
)

// Constant folding: when both operands of a binary op are constants, compute the
// result at compile time and push a constant instead of a deferred node (WARP's
// tryConstantPropagation). Values are kept in an int64; i32 ops operate on the
// low 32 bits and re-sign/zero-extend as needed.

// foldBin folds `a op b` for two integer constants. w selects i64 (else i32).
func foldBin(op wOp, a, b int64, w bool) int64 { return shared.FoldBin(shared.IntOp(op), a, b, w) }
func foldable(op wOp) bool                     { return shared.Foldable(shared.IntOp(op)) }
func foldCompare(op wOp, a, b int64, w bool) int64 {
	return shared.FoldCompare(shared.IntOp(op), a, b, w)
}

// foldUnaryConst folds a unary op over a constant operand: the counting ops
// (clz/ctz/popcnt), eqz, and the width conversions (wrap / sign- & zero-extend).
// typ is the argument pushUnOp received — the operand width for the counting/eqz/
// extend8/16 forms and the result width for wrap/extend32; foldUnaryConst reads
// only the bits each op actually consumes, so the distinction is immaterial here.
// It returns the folded value, its storage type, and ok=false for a non-foldable
// op (floats never reach this path).
func foldUnaryConst(op wOp, a int64, typ machineType) (int64, machineType, bool) {
	w := typ.is64()
	switch op {
	case opEqz: // operand width w; result is i32
		if (w && a == 0) || (!w && uint32(a) == 0) {
			return 1, mtI32, true
		}
		return 0, mtI32, true
	case opClz:
		if w {
			return int64(bits.LeadingZeros64(uint64(a))), mtI64, true
		}
		return int64(bits.LeadingZeros32(uint32(a))), mtI32, true
	case opCtz:
		if w {
			return int64(bits.TrailingZeros64(uint64(a))), mtI64, true
		}
		return int64(bits.TrailingZeros32(uint32(a))), mtI32, true
	case opPopcnt:
		if w {
			return int64(bits.OnesCount64(uint64(a))), mtI64, true
		}
		return int64(bits.OnesCount32(uint32(a))), mtI32, true
	case opWrap: // i32 <- i64: low 32 bits
		return int64(int32(a)), mtI32, true
	case opSExt32: // i64 <- low 32 sign-extended (i64.extend_i32_s / i64.extend32_s)
		return int64(int32(a)), mtI64, true
	case opZExt32: // i64 <- low 32 zero-extended (i64.extend_i32_u)
		return int64(uint32(a)), mtI64, true
	case opSExt8: // sign-extend low 8, result width = operand width
		if w {
			return int64(int8(a)), mtI64, true
		}
		return int64(int32(int8(a))), mtI32, true
	case opSExt16: // sign-extend low 16, result width = operand width
		if w {
			return int64(int16(a)), mtI64, true
		}
		return int64(int32(int16(a))), mtI32, true
	}
	return 0, mtI32, false
}
