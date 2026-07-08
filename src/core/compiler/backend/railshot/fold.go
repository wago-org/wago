package amd64

import "math/bits"

// Constant folding: when both operands of a binary op are constants, compute the
// result at compile time and push a constant instead of a deferred node (WARP's
// tryConstantPropagation). Values are kept in an int64; i32 ops operate on the
// low 32 bits and re-sign/zero-extend as needed.

// foldBin folds `a op b` for two integer constants. w selects i64 (else i32).
func foldBin(op wOp, a, b int64, w bool) int64 {
	if w {
		return foldI64(op, a, b)
	}
	return int64(int32(foldI32(op, uint32(a), uint32(b))))
}

func foldI32(op wOp, a, b uint32) uint32 {
	switch op {
	case opAdd:
		return a + b
	case opSub:
		return a - b
	case opMul:
		return a * b
	case opAnd:
		return a & b
	case opOr:
		return a | b
	case opXor:
		return a ^ b
	case opShl:
		return a << (b & 31)
	case opShrU:
		return a >> (b & 31)
	case opShrS:
		return uint32(int32(a) >> (b & 31))
	case opRotl:
		s := b & 31
		return a<<s | a>>((32-s)&31)
	case opRotr:
		s := b & 31
		return a>>s | a<<((32-s)&31)
	}
	panic("amd64: foldI32 unsupported op")
}

func foldI64(op wOp, a, b int64) int64 {
	ua, ub := uint64(a), uint64(b)
	switch op {
	case opAdd:
		return a + b
	case opSub:
		return a - b
	case opMul:
		return a * b
	case opAnd:
		return a & b
	case opOr:
		return a | b
	case opXor:
		return a ^ b
	case opShl:
		return int64(ua << (ub & 63))
	case opShrU:
		return int64(ua >> (ub & 63))
	case opShrS:
		return a >> (ub & 63)
	case opRotl:
		s := ub & 63
		return int64(ua<<s | ua>>((64-s)&63))
	case opRotr:
		s := ub & 63
		return int64(ua>>s | ua<<((64-s)&63))
	}
	panic("amd64: foldI64 unsupported op")
}

// foldable reports whether op can be constant-folded by foldBin.
func foldable(op wOp) bool {
	switch op {
	case opAdd, opSub, opMul, opAnd, opOr, opXor, opShl, opShrU, opShrS, opRotl, opRotr:
		return true
	}
	return false
}

// foldCompare folds a relational compare of two integer constants to its 0/1
// result. w selects the operand width (i64 else i32); the result is always i32.
// Signed ops interpret the operands at the operand width, unsigned ops at the
// same width unsigned — matching wasm's per-width comparison semantics.
func foldCompare(op wOp, a, b int64, w bool) int64 {
	var res bool
	if w {
		ua, ub := uint64(a), uint64(b)
		switch op {
		case opEq:
			res = a == b
		case opNe:
			res = a != b
		case opLtS:
			res = a < b
		case opLtU:
			res = ua < ub
		case opGtS:
			res = a > b
		case opGtU:
			res = ua > ub
		case opLeS:
			res = a <= b
		case opLeU:
			res = ua <= ub
		case opGeS:
			res = a >= b
		case opGeU:
			res = ua >= ub
		default:
			panic("amd64: foldCompare unsupported op")
		}
	} else {
		sa, sb := int32(a), int32(b)
		ua, ub := uint32(a), uint32(b)
		switch op {
		case opEq:
			res = sa == sb
		case opNe:
			res = sa != sb
		case opLtS:
			res = sa < sb
		case opLtU:
			res = ua < ub
		case opGtS:
			res = sa > sb
		case opGtU:
			res = ua > ub
		case opLeS:
			res = sa <= sb
		case opLeU:
			res = ua <= ub
		case opGeS:
			res = sa >= sb
		case opGeU:
			res = ua >= ub
		default:
			panic("amd64: foldCompare unsupported op")
		}
	}
	if res {
		return 1
	}
	return 0
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
