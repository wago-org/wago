//go:build arm64

package arm64

import "math/bits"

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
	var src [64]Reg
	var pending regMask
	for _, m := range moves {
		if m.dst != m.src {
			src[m.dst] = m.src
			pending = pending.add(m.dst)
		}
	}
	isSource := func(r Reg) bool {
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
			dst := Reg(bits.TrailingZeros64(d))
			if !isSource(dst) {
				emitMove(dst, src[dst])
				pending = pending.remove(dst)
				moved = true
				break
			}
		}
		if moved {
			continue
		}
		// Residual graph is pure cycles; break one with a swap.
		dst := Reg(bits.TrailingZeros64(uint64(pending)))
		s := src[dst]
		emitSwap(dst, s)
		pending = pending.remove(dst)
		for d := uint64(pending); d != 0; d &= d - 1 {
			dd := Reg(bits.TrailingZeros64(d))
			if src[dd] == dst {
				if dd == s {
					pending = pending.remove(dd)
				} else {
					src[dd] = s
				}
			}
		}
	}
}
