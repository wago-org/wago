//go:build amd64

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var scalarRegionsEnabled = os.Getenv("WAGO_EXPERIMENTAL_SCALAR_REGIONS") == "1"

type scalarRegionPending struct {
	left, right storage
	op          wOp
	typ         machineType
	pc          uint32
	index       int
	valid       bool
}
type scalarRegionState struct {
	values  [16]storage
	n       int
	owned   regMask
	pending scalarRegionPending
}

// No spill, local relinquishment, allocation, or code emission on failure.
func (f *fn) scalarRegionFree(s *scalarRegionState, avoid regMask) Reg {
	blocked := avoid.union(s.owned).union(f.pinned).union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if !blocked.has(r) && f.regUser[r] == nil {
			return r
		}
	}
	return regNone
}
func (f *fn) scalarRegionLoad(dst Reg, value storage) {
	switch value.kind {
	case stConst:
		f.loadConst(dst, value)
	case stReg, stLocalReg:
		if dst != value.reg {
			if value.typ.is64() {
				f.a.MovReg64(dst, value.reg)
			} else {
				f.a.MovRegReg32(dst, value.reg)
			}
		}
	case stLocalRef:
		f.loadFrameInt(dst, f.localAddr(value.index()), value.typ)
	default:
		panic("invalid compact scalar value")
	}
}

// Evaluate only a pure previous expression. The final pending expression can
// instead be exported to preserve ordinary destination-aware sink selection.
func (f *fn) materializeScalarRegionPending(s *scalarRegionState) bool {
	p := s.pending
	if !p.valid {
		return true
	}
	left, right := p.left, p.right
	if left.kind == stConst && right.kind == stConst {
		s.values[p.index] = storage{kind: stConst, typ: p.typ, cval: foldBin(p.op, left.cval, right.cval, p.typ.is64())}
		s.pending.valid = false
		return true
	}
	if p.op != opSub && right.kind == stReg && left.kind != stReg {
		left, right = right, left
	}
	dest := regNone
	if left.kind == stReg {
		dest = left.reg
	} else {
		dest = f.scalarRegionFree(s, 0)
	}
	if dest == regNone {
		return false
	}
	scratch := regNone
	if right.kind == stConst && p.typ.is64() && !fitsImm32(right.cval) {
		scratch = f.scalarRegionFree(s, maskOf(dest))
		if scratch == regNone {
			return false
		}
	}
	// All register requirements are satisfied before the first instruction.
	previousPC := f.wasmPC
	f.wasmPC = p.pc
	f.scalarRegionLoad(dest, left)
	w := p.typ.is64()
	if scratch != regNone {
		f.loadConst(scratch, right)
		right = storage{kind: stReg, typ: p.typ, reg: scratch}
	}
	if p.op == opMul {
		switch right.kind {
		case stConst:
			f.a.ImulRI(dest, int32(right.cval), w)
		case stReg, stLocalReg:
			f.a.IMul(dest, right.reg, w)
		case stLocalRef:
			f.a.ImulRM(dest, RSP, f.localAddr(right.index()), w)
		}
	} else {
		enc := aluTable[p.op]
		switch right.kind {
		case stConst:
			f.a.AluRI(enc.digit, dest, int32(right.cval), w)
		case stReg, stLocalReg:
			f.a.AluRR(enc.rr, dest, right.reg, w)
		case stLocalRef:
			f.a.AluRM(enc.rm, dest, RSP, f.localAddr(right.index()), w)
		}
	}
	f.wasmPC = previousPC
	if p.left.kind == stReg {
		s.owned = s.owned.remove(p.left.reg)
	}
	if p.right.kind == stReg {
		s.owned = s.owned.remove(p.right.reg)
	}
	s.owned = s.owned.add(dest)
	s.values[p.index] = storage{kind: stReg, typ: p.typ, reg: dest}
	s.pending.valid = false
	f.stats.peep("scalar-region-evaluate")
	return true
}

func (f *fn) exportScalarRegion(s *scalarRegionState) {
	saved := f.pinned
	f.pinned = f.pinned.union(s.owned)
	push := func(v storage) {
		switch v.kind {
		case stConst:
			f.s.pushIntegerConstant(v.typ, v.cval)
		case stLocalRef, stLocalReg:
			f.pushPlainLocalGet(v.idx)
		case stReg:
			f.pushReg(v.reg, v.typ)
		default:
			panic("invalid compact scalar export")
		}
	}
	for i := 0; i < s.n; i++ {
		if s.pending.valid && s.pending.index == i {
			push(s.pending.left)
			push(s.pending.right)
			pc := f.wasmPC
			f.wasmPC = s.pending.pc
			f.pushBinOp(s.pending.op, s.pending.typ)
			f.wasmPC = pc
		} else {
			push(s.values[i])
		}
	}
	f.pinned = saved
	f.stats.peep("scalar-region-export")
	s.n, s.owned = 0, 0
	s.pending.valid = false
}

func scalarRegionBinary(op byte) (wOp, machineType, bool) {
	typ, base := mtI32, byte(0x6a)
	if op >= 0x7c {
		typ, base = mtI64, 0x7c
	}
	if op < base || int(op-base) >= len(integerBinaryOps) {
		return 0, 0, false
	}
	kind := integerBinaryOps[op-base]
	return kind, typ, kind == opAdd || kind == opSub || kind == opMul || kind == opAnd || kind == opOr || kind == opXor
}

// The body driver owns control/memory/call/exception handling. This helper only
// consumes admitted scalar instructions; pressure exports before the next op.
func (f *fn) tryScalarRegion(r *wasm.Reader) (bool, error) {
	if !scalarRegionsEnabled || !f.scalarRegionNumeric || profileEnabled || regallocCheckEnabled || f.unreachable || f.inlineDepth != 0 || f.localBase != 0 || f.inlineForwardN != 0 || f.inlineTailConstants != nil || len(f.intervalReg) != 0 || f.vectorRegion.enabled || f.pinRelinquished || len(f.customInstructions) != 0 || f.moduleEH || f.gcFrameRoots != nil || len(f.gcTypeLayouts) != 0 || f.storeFwd.valid || f.gcResolved.valid || f.gcSubtypeProofN&gcFreshStructFactMode != 0 || f.s.logicalDepth != 0 || f.s.head.next != f.s.head || f.s.pendingEffects != 0 {
		return false, nil
	}
	s := &f.scratchState().region
	s.n, s.owned = 0, 0
	s.pending.valid = false
	count := 0
	for {
		op, ok := r.Peek()
		if !ok {
			break
		}
		operation, typ, binary := scalarRegionBinary(op)
		if op != 0x20 && op != 0x41 && op != 0x42 && op != 0x1a && !binary {
			break
		}
		// Preserve the existing source-consuming latch priority without guessing
		// whether its structural proof would succeed.
		if op == 0x20 && s.n == 0 && f.opt(optCountedLoopLatch) && !f.interruptible && !f.usesCalls && len(f.ctrl) >= 2 {
			break
		}
		if (op == 0x20 || op == 0x41 || op == 0x42) && s.n == len(s.values) {
			break
		}
		if binary && (s.n < 2 || !f.materializeScalarRegionPending(s)) {
			break
		}
		if op == 0x1a && s.n == 0 {
			break
		}
		pc := f.tracePCBase + uint32(r.Offset())
		f.wasmPC = pc
		_, _ = r.Byte()
		switch op {
		case 0x20:
			x, err := r.U32()
			if err != nil {
				return count != 0, err
			}
			if int(x) >= len(f.localType) {
				return count != 0, fmt.Errorf("scalar region local index out of range")
			}
			var v storage
			if f.localConstZero(int(x)) {
				v = storage{kind: stConst, typ: f.localType[x]}
			} else if pr, fp, pinned := f.pinReg(int(x)); pinned && !fp {
				// Recovery can emit code but cannot allocate an unrelated owner here.
				f.recoverLocal(int(x))
				v = storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: x}
			} else {
				v = storage{kind: stLocalRef, typ: f.localType[x], idx: x}
			}
			s.values[s.n] = v
			s.n++
		case 0x41:
			v, err := r.I32()
			if err != nil {
				return count != 0, err
			}
			if !skipDroppedLiteral(r) {
				s.values[s.n] = storage{kind: stConst, typ: mtI32, cval: int64(v)}
				s.n++
			}
		case 0x42:
			v, err := r.I64()
			if err != nil {
				return count != 0, err
			}
			if !skipDroppedLiteral(r) {
				s.values[s.n] = storage{kind: stConst, typ: mtI64, cval: v}
				s.n++
			}
		case 0x1a:
			s.n--
			if s.pending.valid && s.pending.index == s.n {
				if s.pending.left.kind == stReg {
					s.owned = s.owned.remove(s.pending.left.reg)
				}
				if s.pending.right.kind == stReg {
					s.owned = s.owned.remove(s.pending.right.reg)
				}
				s.pending.valid = false
			} else if s.values[s.n].kind == stReg {
				s.owned = s.owned.remove(s.values[s.n].reg)
			}
		default:
			left, right := s.values[s.n-2], s.values[s.n-1]
			s.n--
			s.pending = scalarRegionPending{left: left, right: right, op: operation, typ: typ, pc: pc, index: s.n - 1, valid: true}
		}
		count++
		f.stats.peep("scalar-region-op")
	}
	if count == 0 {
		return false, nil
	}
	f.exportScalarRegion(s)
	return true, nil
}
