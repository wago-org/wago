//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func (w *machineWindow) append(op machineOp) {
	if int(w.n) == len(w.ops) {
		w.flush()
	}
	w.ops[w.n] = op
	w.n++
}

func (w *machineWindow) flush() {
	for i := uint8(0); i < w.n; {
		op := w.ops[i]
		if w.swapChain != nil && i+1 < w.n {
			next := w.ops[i+1]
			if op.kind == machineSwap && next.kind == machineSwap && op.src == next.dst && op.dst != next.src {
				w.swapChain(op.dst, op.src, next.src)
				w.rewrites++
				i += 2
				continue
			}
		}
		if op.kind == machineSwap {
			w.swap(op.dst, op.src)
		} else {
			w.move(op.dst, op.src)
		}
		i++
	}
	w.n = 0
}

func resolveRegMovesWindow(moves []regMove, emitMove func(dst, src Reg), emitSwap func(a, b Reg), emitSwapChain func(a, b, c Reg)) int {

	w := machineWindow{move: emitMove, swap: emitSwap, swapChain: emitSwapChain}
	resolveRegMoves(moves,
		func(dst, src Reg) { w.append(machineOp{kind: machineMove, dst: dst, src: src}) },
		func(a, b Reg) { w.append(machineOp{kind: machineSwap, dst: a, src: b}) })
	w.flush()
	return w.rewrites
}

// resolveRegMoves emits the moves in a safe order via emitMove (dst = src) and
// emitSwap (exchange). The move set must be a function from dst to src (each
// register written at most once), which holds for ABI argument placement.
//
// The pending graph is held as a fixed src[dst] array plus a `pending` bitmask.
// Besides avoiding a hot-path map allocation, walking destinations by increasing
// register number keeps emitted code independent of Go map iteration order and
// therefore identical between serial and parallel function compilation.
func resolveRegMoves(moves []regMove, emitMove func(dst, src Reg), emitSwap func(a, b Reg)) {
	shared.ResolveRegMoves(len(moves), func(i int) (Reg, Reg) { return moves[i].dst, moves[i].src }, emitMove, emitSwap)
}
