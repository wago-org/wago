//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// Module/worker-owned scratch keeps graph and symbolic operations out of
// per-call closures. The fixed flush points match machineWindow exactly.
type regMoveScratch struct {
	graph      shared.RegMoveGraph[Reg]
	ops        [maxMachineWindow]machineOp
	fpSwapSlot int
}

func (f *fn) emitRegMovesWindow(moves []regMove, floating bool) int {
	if len(moves) == 0 {
		return 0
	}
	s := &f.scratchState().regMoves
	s.graph.Reset()
	s.fpSwapSlot = -1
	for _, m := range moves {
		s.graph.Add(m.dst, m.src)
	}
	n, rewrites := 0, 0
	for {
		dst, src, swap, ok := s.graph.Next()
		if !ok {
			break
		}
		kind := machineMove
		if swap {
			kind = machineSwap
		}
		s.ops[n] = machineOp{kind: kind, dst: dst, src: src}
		n++
		if n == maxMachineWindow {
			rewrites += f.emitRegMoveBatch(s, n, floating)
			n = 0
		}
	}
	return rewrites + f.emitRegMoveBatch(s, n, floating)
}

func (f *fn) emitRegMoveBatch(s *regMoveScratch, n int, floating bool) int {
	rewrites := 0
	for i := 0; i < n; {
		op := s.ops[i]
		if i+1 < n {
			next := s.ops[i+1]
			if op.kind == machineSwap && next.kind == machineSwap && op.src == next.dst && op.dst != next.src {
				if floating {
					if s.fpSwapSlot < 0 {
						s.fpSwapSlot = f.allocSpillSlot()
					}
					off := f.spillOff(s.fpSwapSlot)
					f.fst(SP, off, op.dst, true)
					f.a.FmovReg(op.dst, op.src, true)
					f.a.FmovReg(op.src, next.src, true)
					f.fld(next.src, SP, off, true)
				} else {
					f.a.MovReg64(X16, op.dst)
					f.a.MovReg64(op.dst, op.src)
					f.a.MovReg64(op.src, next.src)
					f.a.MovReg64(next.src, X16)
				}
				rewrites++
				i += 2
				continue
			}
		}
		if op.kind == machineSwap {
			if floating {
				if s.fpSwapSlot < 0 {
					s.fpSwapSlot = f.allocSpillSlot()
				}
				off := f.spillOff(s.fpSwapSlot)
				f.fst(SP, off, op.dst, true)
				f.a.FmovReg(op.dst, op.src, true)
				f.fld(op.src, SP, off, true)
			} else {
				f.a.MovReg64(X16, op.dst)
				f.a.MovReg64(op.dst, op.src)
				f.a.MovReg64(op.src, X16)
			}
		} else if floating {
			f.a.FmovReg(op.dst, op.src, true)
		} else {
			f.a.MovReg64(op.dst, op.src)
		}
		i++
	}
	return rewrites
}
