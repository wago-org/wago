package shared

import "math/bits"

// ResolveRegMoves schedules a parallel move graph with unique destinations
// below 64. Target callbacks retain all physical move and swap behavior.
func ResolveRegMoves[R ~uint8](count int, moveAt func(int) (R, R), emitMove func(dst, src R), emitSwap func(a, b R)) {
	var src [64]R
	var pending uint64
	for i := 0; i < count; i++ {
		dst, source := moveAt(i)
		if dst != source {
			src[dst] = source
			pending |= 1 << dst
		}
	}
	// isSource reports whether r is still needed as some pending move's source.
	isSource := func(r R) bool {
		for d := uint64(pending); d != 0; d &= d - 1 {
			if src[bits.TrailingZeros64(d)] == r {
				return true
			}
		}
		return false
	}
	for pending != 0 {
		moved := false
		for d := uint64(pending); d != 0; d &= d - 1 {
			dst := R(bits.TrailingZeros64(d))
			if !isSource(dst) {
				emitMove(dst, src[dst])
				pending &^= 1 << dst
				moved = true
				break
			}
		}
		if moved {
			continue
		}
		// Residual graph is pure cycles; break one with a swap.
		dst := R(bits.TrailingZeros64(uint64(pending)))
		s := src[dst]
		emitSwap(dst, s)
		pending &^= 1 << dst
		// Any move still sourcing from dst now reads the swapped-in value s.
		for d := uint64(pending); d != 0; d &= d - 1 {
			dd := R(bits.TrailingZeros64(d))
			if src[dd] == dst {
				if dd == s {
					pending &^= 1 << dd
				} else {
					src[dd] = s
				}
			}
		}
	}
}
