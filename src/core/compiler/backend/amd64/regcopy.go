package amd64

// Parallel register move resolution — the Go analogue of WARP's
// common/RegisterCopyResolver.hpp.
//
// When lowering a call we must place several values, each already live in some
// register, into their ABI-mandated argument registers. This is a *parallel*
// move: a target register may still hold a value another move needs as its
// source, and the dependency graph can contain cycles (RAX→RCX while RCX→RAX).
// A naive sequential copy corrupts values. resolveRegMoves orders the copies so
// no move overwrites a still-needed source, breaking pure cycles with swaps.

// regMove requests dst = src (both registers). dst==src is a no-op.
type regMove struct{ dst, src Reg }

// resolveRegMoves emits the moves in a safe order. emitMove(dst, src) must emit
// `dst = src`; emitSwap(a, b) must exchange the contents of a and b. The move
// set must be a function from dst to src (each register written at most once),
// which is always true for ABI argument placement.
func resolveRegMoves(moves []regMove, emitMove func(dst, src Reg), emitSwap func(a, b Reg)) {
	// pending[dst] = src for every non-trivial move still to perform.
	pending := make(map[Reg]Reg, len(moves))
	for _, m := range moves {
		if m.dst != m.src {
			pending[m.dst] = m.src
		}
	}

	isSource := func(r Reg) bool {
		for _, s := range pending {
			if s == r {
				return true
			}
		}
		return false
	}

	for len(pending) > 0 {
		// Emit any move whose destination is not currently needed as a source:
		// writing it cannot clobber a value another pending move still reads.
		moved := false
		for dst, src := range pending {
			if !isSource(dst) {
				emitMove(dst, src)
				delete(pending, dst)
				moved = true
				break // pending mutated; restart the scan
			}
		}
		if moved {
			continue
		}

		// Every remaining destination is also a source: the residual graph is one
		// or more pure cycles. Break one with a swap. After exchanging dst and src,
		// dst holds the value it wanted (src's old contents); src now holds dst's
		// old contents, so any pending move that read dst must now read src.
		var dst, src Reg
		for d, s := range pending {
			dst, src = d, s
			break
		}
		emitSwap(dst, src)
		delete(pending, dst)
		for d, s := range pending {
			if s == dst {
				if d == src {
					delete(pending, d) // became identity
				} else {
					pending[d] = src
				}
			}
		}
	}
}
