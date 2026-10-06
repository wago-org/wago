package shared

// IntOp names integer semantics; targets keep instruction selection separate.
type IntOp uint8

const (
	IntNone IntOp = iota
	IntAdd
	IntSub
	IntAnd
	IntOr
	IntXor
	IntShl
	IntShrU
	IntShrS
	IntRotl
	IntRotr
	IntMul
	IntDivU
	IntDivS
	IntRemU
	IntRemS
	IntClz
	IntCtz
	IntPopcnt
	IntWrap
	IntSExt32
	IntZExt32
	IntSExt8
	IntSExt16
	IntEq
	IntNe
	IntLtS
	IntLtU
	IntGtS
	IntGtU
	IntLeS
	IntLeU
	IntGeS
	IntGeU
	IntEqz
)

func FoldBin(op IntOp, a, b int64, w bool) int64 {
	if w {
		return FoldI64(op, a, b)
	}
	return int64(int32(FoldI32(op, uint32(a), uint32(b))))
}

func FoldI32(op IntOp, a, b uint32) uint32 {
	switch op {
	case IntAdd:
		return a + b
	case IntSub:
		return a - b
	case IntMul:
		return a * b
	case IntAnd:
		return a & b
	case IntOr:
		return a | b
	case IntXor:
		return a ^ b
	case IntShl:
		return a << (b & 31)
	case IntShrU:
		return a >> (b & 31)
	case IntShrS:
		return uint32(int32(a) >> (b & 31))
	case IntRotl:
		s := b & 31
		return a<<s | a>>((32-s)&31)
	case IntRotr:
		s := b & 31
		return a>>s | a<<((32-s)&31)
	}
	panic("shared: FoldI32 unsupported op")
}

func FoldI64(op IntOp, a, b int64) int64 {
	ua, ub := uint64(a), uint64(b)
	switch op {
	case IntAdd:
		return a + b
	case IntSub:
		return a - b
	case IntMul:
		return a * b
	case IntAnd:
		return a & b
	case IntOr:
		return a | b
	case IntXor:
		return a ^ b
	case IntShl:
		return int64(ua << (ub & 63))
	case IntShrU:
		return int64(ua >> (ub & 63))
	case IntShrS:
		return a >> (ub & 63)
	case IntRotl:
		s := ub & 63
		return int64(ua<<s | ua>>((64-s)&63))
	case IntRotr:
		s := ub & 63
		return int64(ua>>s | ua<<((64-s)&63))
	}
	panic("shared: FoldI64 unsupported op")
}

// Foldable reports whether op can be constant-folded by FoldBin.
func Foldable(op IntOp) bool {
	switch op {
	case IntAdd, IntSub, IntMul, IntAnd, IntOr, IntXor, IntShl, IntShrU, IntShrS, IntRotl, IntRotr:
		return true
	}
	return false
}

// FoldCompare folds a relational compare of two integer constants to its 0/1
// result. w selects the operand width (i64 else i32); the result is always i32.
// Signed ops interpret the operands at the operand width, unsigned ops at the
// same width unsigned — matching wasm's per-width comparison semantics.
func FoldCompare(op IntOp, a, b int64, w bool) int64 {
	var res bool
	if w {
		ua, ub := uint64(a), uint64(b)
		switch op {
		case IntEq:
			res = a == b
		case IntNe:
			res = a != b
		case IntLtS:
			res = a < b
		case IntLtU:
			res = ua < ub
		case IntGtS:
			res = a > b
		case IntGtU:
			res = ua > ub
		case IntLeS:
			res = a <= b
		case IntLeU:
			res = ua <= ub
		case IntGeS:
			res = a >= b
		case IntGeU:
			res = ua >= ub
		default:
			panic("shared: FoldCompare unsupported op")
		}
	} else {
		sa, sb := int32(a), int32(b)
		ua, ub := uint32(a), uint32(b)
		switch op {
		case IntEq:
			res = sa == sb
		case IntNe:
			res = sa != sb
		case IntLtS:
			res = sa < sb
		case IntLtU:
			res = ua < ub
		case IntGtS:
			res = sa > sb
		case IntGtU:
			res = ua > ub
		case IntLeS:
			res = sa <= sb
		case IntLeU:
			res = ua <= ub
		case IntGeS:
			res = sa >= sb
		case IntGeU:
			res = ua >= ub
		default:
			panic("shared: FoldCompare unsupported op")
		}
	}
	if res {
		return 1
	}
	return 0
}
