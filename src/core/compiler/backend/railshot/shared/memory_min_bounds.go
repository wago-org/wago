package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// UnsignedOperationMaximum conservatively bounds modular integer operations.
// Inputs are unsigned upper bounds, not bit masks. Saturation preserves safety
// when an addition, product or shift might wrap. No source operation is removed.
func UnsignedOperationMaximum(op IntOp, a, b uint64, rightConstant uint64, rightIsConstant, wide bool) uint64 {
	limit := ^uint64(0)
	shiftMask := uint64(63)
	if !wide {
		limit = uint64(^uint32(0))
		shiftMask = 31
	}
	if a > limit {
		a = limit
	}
	if b > limit {
		b = limit
	}
	switch op {
	case IntAnd:
		if a < b {
			return a
		}
		return b
	case IntOr, IntXor:
		// An interval bound is not a bit mask: values <=5 can set bit 1.
		envelope := func(x uint64) uint64 {
			x |= x >> 1
			x |= x >> 2
			x |= x >> 4
			x |= x >> 8
			x |= x >> 16
			x |= x >> 32
			return x
		}
		return envelope(a) | envelope(b)
	case IntAdd:
		if b <= limit-a {
			return a + b
		}
	case IntMul:
		if b == 0 {
			return 0
		}
		if a <= limit/b {
			return a * b
		}
	case IntShl:
		if rightIsConstant {
			shift := rightConstant & shiftMask
			if a <= limit>>shift {
				return a << shift
			}
		}
	case IntShrU:
		if rightIsConstant {
			return a >> (rightConstant & shiftMask)
		}
		return a
	}
	return limit
}

// ProvenMinimumAccess uses only the selected declaration's immutable minimum.
// It never depends on a cached shared extent or on growth by another instance.
// Subtractions validate the entire access without overflowing address arithmetic.
func ProvenMinimumAccess(mt wasm.MemType, maximum, offset uint64, size int) bool {
	if size <= 0 || mt.Limits.Min > ^uint64(0)>>16 || (!mt.Limits.Addr64 && mt.Limits.Min > 65536) {
		return false
	}
	minimum := mt.Limits.Min << 16
	if uint64(size) > minimum {
		return false
	}
	available := minimum - uint64(size)
	return offset <= available && maximum <= available-offset
}
