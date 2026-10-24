package shared

import "math/bits"

// RegMoveGraph schedules parallel moves with unique destinations below 64.
// Its zero value is empty. Add every input before calling Next. Reset permits
// reusing the same storage without retaining callbacks or allocation owners.
type RegMoveGraph[R ~uint8] struct {
	src     [64]R
	pending uint64
}

func (g *RegMoveGraph[R]) Reset() { g.pending = 0 }

func (g *RegMoveGraph[R]) Add(dst, source R) {
	if dst != source {
		g.src[dst] = source
		g.pending |= uint64(1) << dst
	}
}

// Next returns a move or swap in deterministic destination order. A swap uses
// both registers' old values; a move assigns dst = source. ok is false at end.
func (g *RegMoveGraph[R]) Next() (dst, source R, swap, ok bool) {
	if g.pending == 0 {
		return 0, 0, false, false
	}
	for d := g.pending; d != 0; d &= d - 1 {
		target := R(bits.TrailingZeros64(d))
		needed := false
		for uses := g.pending; uses != 0; uses &= uses - 1 {
			if g.src[bits.TrailingZeros64(uses)] == target {
				needed = true
				break
			}
		}
		if !needed {
			g.pending &^= uint64(1) << target
			return target, g.src[target], false, true
		}
	}
	target := R(bits.TrailingZeros64(g.pending))
	s := g.src[target]
	g.pending &^= uint64(1) << target
	for d := g.pending; d != 0; d &= d - 1 {
		dd := R(bits.TrailingZeros64(d))
		if g.src[dd] == target {
			if dd == s {
				g.pending &^= uint64(1) << dd
			} else {
				g.src[dd] = s
			}
		}
	}
	return target, s, true, true
}

// ResolveRegMoves schedules parallel moves with unique destinations and
// registers below 64. The callbacks retain each target's physical move/swap
// behavior; RegMoveGraph provides the same schedule without captured callbacks.
func ResolveRegMoves[R ~uint8](count int, moveAt func(int) (R, R), emitMove func(dst, src R), emitSwap func(a, b R)) {
	var graph RegMoveGraph[R]
	for i := 0; i < count; i++ {
		dst, src := moveAt(i)
		graph.Add(dst, src)
	}
	for {
		dst, src, swap, ok := graph.Next()
		if !ok {
			return
		}
		if swap {
			emitSwap(dst, src)
		} else {
			emitMove(dst, src)
		}
	}
}
