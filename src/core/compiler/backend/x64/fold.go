package x64

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
	panic("x64: foldI32 unsupported op")
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
	panic("x64: foldI64 unsupported op")
}

// foldable reports whether op can be constant-folded by foldBin.
func foldable(op wOp) bool {
	switch op {
	case opAdd, opSub, opMul, opAnd, opOr, opXor, opShl, opShrU, opShrS, opRotl, opRotr:
		return true
	}
	return false
}
