package amd64

// Parallel register-move resolution (WARP's RegisterCopyResolver): placing N
// values, each already live in some register, into their target registers is a
// *parallel* move — a target may still hold a value another move needs — and the
// dependency graph can contain cycles. resolveRegMoves orders the copies so no
// move overwrites a still-needed source, breaking pure cycles with a swap. Used
// to marshal call arguments into the register-ABI argument registers.

// regMove requests dst = src. dst == src is a no-op.
type regMove struct{ dst, src Reg }

// resolveRegMoves emits the moves in a safe order via emitMove (dst = src) and
// emitSwap (exchange). The move set must be a function from dst to src (each
// register written at most once), which holds for ABI argument placement.
func resolveRegMoves(moves []regMove, emitMove func(dst, src Reg), emitSwap func(a, b Reg)) {
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
		moved := false
		for dst, src := range pending {
			if !isSource(dst) {
				emitMove(dst, src)
				delete(pending, dst)
				moved = true
				break
			}
		}
		if moved {
			continue
		}
		// Residual graph is pure cycles; break one with a swap.
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
					delete(pending, d)
				} else {
					pending[d] = src
				}
			}
		}
	}
}
