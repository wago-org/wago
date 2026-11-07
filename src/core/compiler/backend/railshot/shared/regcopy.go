package shared

import "math/bits"

// ResolveRegMoves schedules a parallel move graph with unique destinations
// below 64. Target callbacks retain all physical move and swap behavior.
func ResolveRegMoves[R ~uint8](count int, moveAt func(int) (R, R), emitMove func(dst, src R), emitSwap func(a, b R)) {
	var src [64]R
	var pending, sources uint64
	for i := 0; i < count; i++ {
		dst, source := moveAt(i)
		if dst != source {
			src[dst] = source
			pending |= 1 << dst
			sources |= 1 << source
		}
	}

	// A destination can move once no remaining move needs its old value.
	// Track ready destinations instead of repeatedly searching every candidate.
	ready := pending &^ sources
	for ready != 0 {
		dst := R(bits.TrailingZeros64(ready))
		source := src[dst]
		emitMove(dst, source)
		pending &^= 1 << dst
		ready &= ready - 1
		sourceBit := uint64(1) << source
		if pending&sourceBit == 0 {
			continue
		}
		used := false
		for d := pending; d != 0; d &= d - 1 {
			if src[bits.TrailingZeros64(d)] == source {
				used = true
				break
			}
		}
		if !used {
			ready |= sourceBit
		}
	}
	// With unique destinations, the residual graph consists solely of cycles.
	// Walk each cycle once, rotating its values through adjacent swaps.
	for pending != 0 {
		start := R(bits.TrailingZeros64(pending))
		dst := start
		source := src[dst]
		for source != start {
			emitSwap(dst, source)
			pending &^= 1 << dst
			dst = source
			source = src[dst]
		}
		pending &^= 1 << dst
	}
}
