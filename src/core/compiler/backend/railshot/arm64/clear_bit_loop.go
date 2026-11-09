//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var clearBitLoopEnabled = os.Getenv("WAGO_ARM64_NO_CLEAR_BIT_LOOP") != "1"

type clearBitUpdate struct {
	local int
	step  uint32
}
type clearBitPlan struct {
	bit     int
	updates [4]clearBitUpdate
	n       int
}

// Inspect one bounded, effect-free iteration. Other locals may only advance by
// constants; the terminating local clears its lowest set bit. The copied reader
// and private records never change the ordinary bytecode driver's state.
func (f *fn) inspectClearBitLoop(source *wasm.Reader) (p clearBitPlan, ok bool) {
	r := *source
	start := r.Offset()
	op := func(want byte) bool {
		if r.Offset()-start >= 128 || f.integerResidualWork >= integerResidualWorkLimit {
			return false
		}
		f.integerResidualWork++
		got, e := r.Byte()
		return e == nil && got == want
	}
	local := func() (int, bool) {
		v, e := r.U32()
		return int(v), e == nil && v < uint32(f.nLocals) && f.localType[v] == mtI32
	}
	for p.n <= len(p.updates) {
		if !op(0x20) {
			return p, false
		}
		index, yes := local()
		if !yes || !op(0x41) {
			return p, false
		}
		step, e := r.I32()
		if e != nil || !op(0x6a) {
			return p, false
		}
		// A set closes an affine update; a second get begins (x-1)&x.
		if r.Offset()-start >= 128 || f.integerResidualWork >= integerResidualWorkLimit {
			return p, false
		}
		f.integerResidualWork++
		next, e := r.Byte()
		if e != nil {
			return p, false
		}
		if next == 0x21 {
			target, yes := local()
			if !yes || target != index || p.n == len(p.updates) {
				return p, false
			}
			for _, u := range p.updates[:p.n] {
				if u.local == index {
					return p, false
				}
			}
			p.updates[p.n] = clearBitUpdate{index, uint32(step)}
			p.n++
			continue
		}
		if next != 0x20 || step != -1 {
			return p, false
		}
		other, yes := local()
		if !yes || other != index || !op(0x71) || !op(0x22) {
			return p, false
		}
		target, yes := local()
		if !yes || target != index || !op(0x0d) {
			return p, false
		}
		label, e := r.U32()
		if e != nil || label != 0 || !op(0x0b) {
			return p, false
		}
		for _, u := range p.updates[:p.n] {
			if u.local == index {
				return p, false
			}
		}
		p.bit = index
		return p, true
	}
	return p, false
}

func (f *fn) emitClearBitLoop(source *wasm.Reader) {
	if !clearBitLoopEnabled || !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() || f.a.Len() > 64<<10 {
		return
	}
	if len(f.intervalLast) != 0 && (!integerMapIntervalsEnabled || !f.intervalControl || f.intervalActive != 0) {
		return
	}
	p, ok := f.inspectClearBitLoop(source)
	if !ok {
		return
	}
	// Do not add spills to the ordinary path when scratch is unavailable.
	free := false
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if f.regUser[r] == nil && !blocked.has(r) {
			free = true
			break
		}
	}
	fpFree := false
	fpBlocked := f.blockedFRegs(0)
	for _, r := range fpAllocRegs {
		if f.fregUser[r] == nil && !fpBlocked.has(r) {
			fpFree = true
			break
		}
	}
	if !free || !fpFree {
		return
	}
	oldPins, oldFPins := f.pinned, f.fpinned
	count := f.allocRegOrNone(0)
	f.pinned = f.pinned.add(count)
	v := f.allocFReg(0)
	f.fpinned = f.fpinned.add(v)
	defer func() { f.pinned, f.fpinned = oldPins, oldFPins; f.release(count); f.releaseF(v) }()
	f.recoverLocal(p.bit)
	for _, u := range p.updates[:p.n] {
		f.recoverLocal(u.local)
	}
	read := func(local int, out Reg) {
		if r, _, yes := f.pinReg(local); yes {
			f.a.MovReg32(out, r)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm32(out, 0)
		} else {
			f.ld32(out, SP, f.localOff(local))
		}
	}
	publish := func(local int, in Reg) {
		if r, _, yes := f.pinReg(local); yes {
			f.a.MovReg32(r, in)
			f.markLocalDirty(local)
		} else {
			f.st64(SP, f.localOff(local), in)
			f.locals[local].state = lsMem
		}
		f.setFactsForLocal(local, 0)
	}
	read(p.bit, count)
	f.a.FmovFromGpr(v, count, false)
	f.a.Cnt8b(v, v)
	f.a.Addv8b(v, v)
	f.a.FmovToGpr(count, v, false)
	// A source do-while runs once even for zero. Execute max(popcount(x),1)-1
	// iterations here, then keep one original iteration to restore exact exits.
	f.a.SubsImm32(count, count, 1)
	f.a.Csel32(count, count, ZR, condAE)
	for _, u := range p.updates[:p.n] {
		if u.step == 0 {
			continue
		}
		read(u.local, X16)
		switch u.step {
		case 1:
			f.a.Add32(X16, X16, count)
		case ^uint32(0):
			f.a.Sub32(X16, X16, count)
		default:
			f.a.MovImm32(X17, int32(u.step))
			f.a.Mul32(X17, count, X17)
			f.a.Add32(X16, X16, X17)
		}
		publish(u.local, X16)
	}
	f.a.MovImm32(X16, 1)
	publish(p.bit, X16)
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("clear-bit-loop-prefix")
}
