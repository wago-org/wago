//go:build amd64

package amd64

import "math/bits"

// Parallel register-move resolution (WARP's RegisterCopyResolver): placing N
// values, each already live in some register, into their target registers is a
// *parallel* move — a target may still hold a value another move needs — and the
// dependency graph can contain cycles. resolveRegMoves orders the copies so no
// move overwrites a still-needed source, breaking pure cycles with a swap. Used
// to marshal call arguments into the register-ABI argument registers.

// regMove requests dst = src. dst == src is a no-op.
type regMove struct{ dst, src Reg }

const maxMachineWindow = 24

type machineOpKind uint8

const (
	machineMove machineOpKind = iota
	machineSwap
)

type machineOp struct {
	kind     machineOpKind
	dst, src Reg
}

// machineWindow is the first fixed-capacity symbolic machine-operation seam.
// It covers resolved ABI register shuffles ending at an explicit call boundary.
// Capacity exhaustion flushes in order, so correctness does not depend on
// retaining the whole shuffle and ordinary compilation allocates nothing.
type machineWindow struct {
	ops  [maxMachineWindow]machineOp
	n    uint8
	move func(dst, src Reg)
	swap func(a, b Reg)
}

func (w *machineWindow) append(op machineOp) {
	if int(w.n) == len(w.ops) {
		w.flush()
	}
	w.ops[w.n] = op
	w.n++
}

func (w *machineWindow) flush() {
	for i := uint8(0); i < w.n; i++ {
		op := w.ops[i]
		if op.kind == machineSwap {
			w.swap(op.dst, op.src)
		} else {
			w.move(op.dst, op.src)
		}
	}
	w.n = 0
}

func resolveRegMovesWindow(moves []regMove, emitMove func(dst, src Reg), emitSwap func(a, b Reg)) {
	w := machineWindow{move: emitMove, swap: emitSwap}
	resolveRegMoves(moves,
		func(dst, src Reg) { w.append(machineOp{kind: machineMove, dst: dst, src: src}) },
		func(a, b Reg) { w.append(machineOp{kind: machineSwap, dst: a, src: b}) })
	w.flush()
}

// resolveRegMoves emits the moves in a safe order via emitMove (dst = src) and
// emitSwap (exchange). The move set must be a function from dst to src (each
// register written at most once), which holds for ABI argument placement.
//
// The pending graph is held as a fixed src[dst] array plus a `pending` bitmask
// of live destinations (every dst is a GP register < 64), so resolution allocates
// nothing — this runs on every register-ABI call.
func resolveRegMoves(moves []regMove, emitMove func(dst, src Reg), emitSwap func(a, b Reg)) {
	var src [64]Reg
	var pending regMask
	for _, m := range moves {
		if m.dst != m.src {
			src[m.dst] = m.src
			pending = pending.add(m.dst)
		}
	}
	// isSource reports whether r is still needed as some pending move's source.
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
		// Any move still sourcing from dst now reads the swapped-in value s.
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
