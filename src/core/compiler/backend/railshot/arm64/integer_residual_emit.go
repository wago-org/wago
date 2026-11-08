//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"math/bits"
	"os"
)

var integerResidualVectorDivEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_RESIDUAL_VECTOR_DIV") != "1"

func (f *fn) hasIntegerResidualPrefix(source *wasm.Reader) bool {
	r := *source
	var immediate wasm.InstructionImmediate
	candidate := false
	for n := 0; n < 24 && f.integerResidualWork < integerResidualWorkLimit; n++ {
		f.integerResidualWork++
		op, e := r.Byte()
		if e != nil {
			return false
		}
		switch op {
		case 0x6c, 0x73:
			candidate = true
		case 0x20, 0x21, 0x22, 0x41, 0x6a, 0x6b, 0x6d, 0x71, 0x72, 0x74, 0x76, 0x47:
			if f.classifier.ClassifyInto(&r, op, &immediate) != nil {
				return false
			}
		case 0x0d:
			return candidate
		default:
			return false
		}
	}
	return candidate
}

func (f *fn) emitIntegerResidualLoop(source *wasm.Reader) {
	if f.integerResidualPlans >= 2 || f.a.Len() > 64<<10 || !integerResidualEnabled || !f.opt(optDotLoopVector) || f.interruptible || f.moduleEH || f.localBase != 0 || len(f.customInstructions) != 0 || f.tracksGCFrameRoots() {
		return
	}
	if len(f.intervalLast) != 0 && (!integerMapIntervalsEnabled || !f.intervalControl || f.intervalActive != 0) {
		return
	}
	if !f.hasIntegerResidualPrefix(source) {
		return
	}
	f.emitIntegerResidualBody(source)
}

// Keep the large workspace off the ordinary prefix-rejection stack path.
func (f *fn) emitIntegerResidualBody(source *wasm.Reader) {
	p, ok := f.inspectIntegerResidual(source)
	if !ok {
		return
	}
	f.stats.peep("integer-residual-recognized")
	f.emitIntegerResidualPlan(&p)
}
func (f *fn) emitIntegerResidualPlan(p *integerResidualPlan) {
	// Account for the exact chosen topological schedule and the later linear
	// accumulators before touching native or allocator state.
	uses := p.uses
	live, peak := 0, 0
	consume := func(x int) {
		uses[x]--
		if uses[x] == 0 {
			live--
		}
	}
	for i, n := range p.nodes[:p.n] {
		if !p.used[i] {
			continue
		}
		live++
		peak = max(peak, live)
		if n.Op != 0x20 && n.Op != 0x41 {
			consume(n.Left)
			if n.Op != 0x74 && n.Op != 0x76 {
				consume(n.Right)
			}
		}
	}
	neededFP := max(peak, live+p.mapN+1) + 1 // one persistent lane-offset vector
	available := 0
	blocked := f.blockedFRegs(0)
	for _, r := range fpAllocRegs {
		if !blocked.has(r) && f.fregUser[r] == nil {
			available++
		}
	}
	if available < neededFP {
		f.stats.peep("integer-residual-fp-reject")
		return
	}
	free := 0
	gpBlocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if f.regUser[r] == nil && !gpBlocked.has(r) {
			free++
		}
	}
	if free < 6 {
		f.stats.peep("integer-residual-gp-reject")
		return
	}
	oldPins, oldFPins := f.pinned, f.fpinned
	var gp [6]Reg
	for i := range gp {
		gp[i] = regNone
	}
	defer func() {
		f.pinned, f.fpinned = oldPins, oldFPins
		for _, r := range gp {
			if r != regNone {
				f.release(r)
			}
		}
	}()
	for i := range gp {
		gp[i] = f.allocRegOrNone(0)
		if gp[i] == regNone {
			return
		}
		f.pinned = f.pinned.add(gp[i])
	}
	count, seed, hash, value, multiplier, magicReg := gp[0], gp[1], gp[2], gp[3], gp[4], gp[5]
	read := func(local int, out Reg) {
		if r, _, ok := f.pinReg(local); ok {
			f.a.MovReg32(out, r)
		} else if f.locals[local].state == lsConstZero {
			f.a.MovImm32(out, 0)
		} else {
			f.ld32(out, SP, f.localOff(local))
		}
	}
	for _, cell := range p.locals[:p.localN] {
		f.recoverLocal(int(cell.Index))
	}
	counterLocal, seedLocal, hashLocal := int(p.locals[p.counter].Index), int(p.locals[p.seed].Index), int(p.locals[p.hash].Index)
	read(counterLocal, count)
	f.cmpImm(count, 17, false)
	scalar := f.a.Bcond(condB)
	read(seedLocal, seed)
	read(hashLocal, hash)
	f.a.MovImm32(multiplier, int32(p.multiplier))
	var magic uint64
	var shift uint
	var correction bool
	if p.divisor > 1 && p.divisor&(p.divisor-1) != 0 {
		m, s, c := railshot.MagicS(uint64(p.divisor), 32)
		magic, shift, correction = uint64(m), s, c
		f.a.MovImm32(magicReg, int32(magic))
	}
	vectorDivision := integerResidualVectorDivEnabled && p.divisor > 1 && p.divisor&(p.divisor-1) != 0 && !correction && int32(magic) > 0 && shift < 31
	alloc := func() Reg { r := f.allocFReg(0); f.fpinned = f.fpinned.add(r); return r }
	release := func(r Reg) { f.fpinned = f.fpinned.remove(r); f.releaseF(r) }
	lanes := alloc()
	for lane := byte(0); lane < 4; lane++ {
		f.a.MovImm32(X16, int32(uint32(lane)*p.seedStep))
		f.a.NeonInsS(lanes, X16, lane)
	}
	loop := f.a.Len()
	uses = p.uses
	var values [256]Reg
	done := func(x int) {
		uses[x]--
		if uses[x] == 0 {
			release(values[x])
		}
	}
	broadcast := func(r Reg, c uint32) { f.a.MovImm32(X16, int32(c)); f.a.NeonDupGprS(r, X16) }
	for i, n := range p.nodes[:p.n] {
		if !p.used[i] {
			continue
		}
		out := alloc()
		values[i] = out
		if n.Op == 0x20 {
			if n.Left == seedLocal {
				f.a.NeonDupGprS(out, seed)
				f.a.NeonAddS(out, out, lanes)
			} else {
				read(n.Left, X16)
				f.a.NeonDupGprS(out, X16)
			}
			continue
		}
		if n.Op == 0x41 {
			broadcast(out, uint32(n.Bits))
			continue
		}
		a, b := values[n.Left], values[n.Right]
		switch n.Op {
		case 0x6a:
			f.a.NeonAddS(out, a, b)
		case 0x6b:
			f.a.NeonSubS(out, a, b)
		case 0x6c:
			f.a.NeonMulS(out, a, b)
		case 0x71:
			f.a.NeonAnd16b(out, a, b)
		case 0x72:
			f.a.NeonOrr16b(out, a, b)
		case 0x73:
			f.a.NeonEor16b(out, a, b)
		case 0x74, 0x76:
			s := uint8(p.nodes[n.Right].Bits & 31)
			if s == 0 {
				f.a.NeonMov16b(out, a)
			} else if n.Op == 0x74 {
				f.a.NeonShlS(out, a, s)
			} else {
				f.a.NeonUshrS(out, a, s)
			}
		}
		done(n.Left)
		if n.Op != 0x74 && n.Op != 0x76 {
			done(n.Right)
		}
	}
	var outputs [4]Reg
	for i, m := range p.maps[:p.mapN] {
		out := alloc()
		outputs[i] = out
		scratch := alloc()
		initialized := false
		if m.constant != 0 {
			broadcast(out, m.constant)
			initialized = true
		}
		for _, term := range m.terms[:m.n] {
			if term.coefficient == 0 {
				continue
			}
			input := values[term.node]
			if !initialized {
				if term.coefficient == 1 {
					f.a.NeonMov16b(out, input)
				} else {
					broadcast(scratch, term.coefficient)
					f.a.NeonMulS(out, input, scratch)
				}
				initialized = true
			} else {
				if term.coefficient == 1 {
					f.a.NeonAddS(out, out, input)
				} else {
					broadcast(scratch, term.coefficient)
					f.a.NeonMulS(scratch, input, scratch)
					f.a.NeonAddS(out, out, scratch)
				}
			}
			done(term.node)
		}
		if !initialized {
			broadcast(out, 0)
		}
		if vectorDivision && m.divisor == p.divisor {
			// With positive signed magic m, doubling n*m never saturates.
			// SQDMULH gives floor(n*m/2^31); ASR(s+1) is exactly
			// SMULL(n,m)>>(32+s). Add the logical sign bit for truncation
			// toward zero. No floating arithmetic or reassociation is used.
			f.a.NeonDupGprS(scratch, magicReg)
			f.a.NeonSqdmulhS(out, out, scratch)
			f.a.NeonSshrS(out, out, uint8(shift+1))
			f.a.NeonUshrS(scratch, out, 31)
			f.a.NeonAddS(out, out, scratch)
		}
		release(scratch)
	}
	// These updates intentionally remain scalar and in original iteration order.
	// Positive constant division cannot trap; its exact signed magic sequence
	// mirrors the existing direct quotient lowering, without changing rounding.
	for lane := byte(0); lane < 4; lane++ {
		for i, m := range p.maps[:p.mapN] {
			f.a.NeonUmovS(value, outputs[i], lane)
			if m.divisor > 1 && !(vectorDivision && m.divisor == p.divisor) {
				if m.divisor&(m.divisor-1) == 0 {
					f.a.AsrImm(X16, value, 31, true)
					f.a.AndImm32(X16, X16, m.divisor-1)
					f.a.Add32(value, value, X16)
					f.a.AsrImm(value, value, uint8(bits.TrailingZeros32(m.divisor)), true)
				} else {
					f.a.Smull(X16, value, magicReg)
					if correction {
						f.a.AsrImm64(X16, X16, 32)
						f.a.Add32(X16, X16, value)
						if shift != 0 {
							f.a.AsrImm(X16, X16, uint8(shift), true)
						}
					} else {
						f.a.AsrImm64(X16, X16, uint8(32+shift))
					}
					f.a.AddShiftedReg(value, X16, X16, a64.RegShiftLSR, 31, true)
				}
			}
			f.aluRR(opXor, hash, value, false)
			f.a.Mul32(hash, hash, multiplier)
		}
	}
	for _, r := range outputs[:p.mapN] {
		release(r)
	}
	f.floatMapOffset(seed, seed, uint64(4*p.seedStep), false, X16)
	f.a.SubImm32(count, count, 4)
	f.cmpImm(count, 5, false)
	f.patchBranch19(f.a.Bcond(condAE), loop)
	// One to four original iterations restore every changed temporary as well as
	// the final recurrence state, without relying on function-wide liveness.
	publish := func(local int, r Reg) {
		if dst, _, ok := f.pinReg(local); ok {
			f.a.MovReg32(dst, r)
			f.markLocalDirty(local)
		} else {
			f.st64(SP, f.localOff(local), r)
			f.locals[local].state = lsMem
		}
		f.setFactsForLocal(local, 0)
	}
	publish(counterLocal, count)
	publish(seedLocal, seed)
	publish(hashLocal, hash)
	f.patchBranch19(scalar, f.a.Len())
	release(lanes)
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	if vectorDivision {
		f.stats.peep("integer-residual-vector-div")
	}
	f.integerResidualPlans++
	f.stats.peep("integer-residual-vector")
}
