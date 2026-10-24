//go:build arm64

package arm64

// unsignedAddressBound inspects at most sixteen deferred nodes. It does not
// materialize operands or discard their effects; unknown producers fail closed.
// Return the unused budget so recursive proofs do not heap-box a counter in TinyGo.
func (f *fn) unsignedAddressBound(e *elem, remaining int) (uint64, bool, int) {
	if e == nil || e.st.typ != mtI32 || remaining <= 0 {
		return 0, false, remaining
	}
	remaining--
	if e.elemKind() != ekValue && e.elemKind() != ekDeferred {
		return 0, false, remaining
	}
	if e.st.valueFacts().has(factBoolean) {
		return 1, true, remaining
	}
	if e.elemKind() == ekValue {
		if e.st.kind == stConst {
			return uint64(uint32(e.st.cval)), true, remaining
		}
		if e.st.kind == stMemRef && !e.st.memSigned() && e.st.memSize() > 0 && e.st.memSize() < 4 {
			return (uint64(1) << uint(8*e.st.memSize())) - 1, true, remaining
		}
		return 0, false, remaining
	}
	if e.elemKind() != ekDeferred {
		return 0, false, remaining
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
			return uint64(mask), true, remaining
		}
		if mask, ok := constant(a); ok {
			return uint64(mask), true, remaining
		}
		bound, ok, left := f.unsignedAddressBound(a, remaining)
		remaining = left
		if ok {
			return bound, true, remaining
		}
		return f.unsignedAddressBound(b, remaining)
	case opShrU:
		shift, ok := constant(b)
		if !ok {
			return 0, false, remaining
		}
		bound := uint64(^uint32(0))
		known, ok, left := f.unsignedAddressBound(a, remaining)
		remaining = left
		if ok {
			bound = known
		}
		return bound >> (shift & 31), true, remaining
	case opShl:
		shift, ok := constant(b)
		if !ok {
			return 0, false, remaining
		}
		bound, ok, remaining := f.unsignedAddressBound(a, remaining)
		if !ok {
			return 0, false, remaining
		}
		bound <<= shift & 31
		if bound > 0xffffffff {
			return 0, false, remaining
		}
		return bound, true, remaining
	case opAdd, opMul:
		x, ok, remaining := f.unsignedAddressBound(a, remaining)
		if !ok {
			return 0, false, remaining
		}
		y, ok, remaining := f.unsignedAddressBound(b, remaining)
		if !ok {
			return 0, false, remaining
		}
		if e.deferredOp() == opAdd {
			x += y
		} else {
			x *= y
		}
		if x > 0xffffffff {
			return 0, false, remaining
		}
		return x, true, remaining
	}
	return 0, false, remaining
}

func (f *fn) staticallyInBounds(off uint64, size int) bool {
	if f.m == nil || f.s == nil || off > 0xffffffff || size <= 0 {
		return false
	}
	bound, ok, _ := f.unsignedAddressBound(f.s.back(), 16)
	if !ok {
		return false
	}
	memory, ok := f.m.MemoryType(0)
	return ok && !memory.Limits.Addr64 && bound+off+uint64(size) <= memory.Limits.Min<<16
}
