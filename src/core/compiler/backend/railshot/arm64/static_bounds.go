//go:build arm64

package arm64

// unsignedAddressBound inspects at most sixteen deferred nodes. It does not
// materialize operands or discard their effects; unknown producers fail closed.
func (f *fn) unsignedAddressBound(e *elem, remaining *int) (uint64, bool) {
	if e == nil || e.st.typ != mtI32 || *remaining <= 0 {
		return 0, false
	}
	(*remaining)--
	if e.elemKind() != ekValue && e.elemKind() != ekDeferred {
		return 0, false
	}
	if e.st.valueFacts().has(factBoolean) {
		return 1, true
	}
	if e.elemKind() == ekValue {
		if e.st.kind == stConst {
			return uint64(uint32(e.st.cval)), true
		}
		if e.st.kind == stMemRef && !e.st.memSigned() && e.st.memSize() > 0 && e.st.memSize() < 4 {
			return (uint64(1) << uint(8*e.st.memSize())) - 1, true
		}
		return 0, false
	}
	if e.elemKind() != ekDeferred {
		return 0, false
	}
	a, b := f.s.arg0(e), f.s.arg1(e)
	constant := func(x *elem) (uint32, bool) {
		if x != nil && x.elemKind() == ekValue && x.st.kind == stConst && x.st.typ == mtI32 {
			return uint32(x.st.cval), true
		}
		return 0, false
	}
	switch e.deferredOp() {
	case opAnd:
		if mask, ok := constant(b); ok {
			return uint64(mask), true
		}
		if mask, ok := constant(a); ok {
			return uint64(mask), true
		}
		if bound, ok := f.unsignedAddressBound(a, remaining); ok {
			return bound, true
		}
		return f.unsignedAddressBound(b, remaining)
	case opShrU:
		shift, ok := constant(b)
		if !ok {
			return 0, false
		}
		bound := uint64(^uint32(0))
		if known, ok := f.unsignedAddressBound(a, remaining); ok {
			bound = known
		}
		return bound >> (shift & 31), true
	case opShl:
		shift, ok := constant(b)
		if !ok {
			return 0, false
		}
		bound, ok := f.unsignedAddressBound(a, remaining)
		if !ok {
			return 0, false
		}
		bound <<= shift & 31
		if bound > 0xffffffff {
			return 0, false
		}
		return bound, true
	case opAdd, opMul:
		x, ok := f.unsignedAddressBound(a, remaining)
		if !ok {
			return 0, false
		}
		y, ok := f.unsignedAddressBound(b, remaining)
		if !ok {
			return 0, false
		}
		if e.deferredOp() == opAdd {
			x += y
		} else {
			x *= y
		}
		if x > 0xffffffff {
			return 0, false
		}
		return x, true
	}
	return 0, false
}

func (f *fn) staticallyInBounds(off uint64, size int) bool {
	if f.m == nil || f.s == nil || off > 0xffffffff || size <= 0 {
		return false
	}
	remaining := 16
	bound, ok := f.unsignedAddressBound(f.s.back(), &remaining)
	if !ok {
		return false
	}
	memory, ok := f.m.MemoryType(0)
	return ok && !memory.Limits.Addr64 && bound+off+uint64(size) <= memory.Limits.Min<<16
}
