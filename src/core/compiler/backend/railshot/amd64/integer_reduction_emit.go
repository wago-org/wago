//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"runtime"
)

// Linux uses the qualified packet by default. Zero disables it; one permits
// explicit qualification on other amd64 operating systems. The CPU gate stays.
var integerReductionLoopEnabled = integerReductionLoopDefault(runtime.GOOS, os.Getenv("WAGO_AMD64_INTEGER_REDUCTION_LOOP"))

func integerReductionLoopDefault(goos, setting string) bool {
	return setting == "1" || goos == "linux" && setting != "0"
}

const integerReductionMinTripCount = 16

func (f *fn) tryIntegerReductionLoop(r *wasm.Reader) (bool, error) {
	if f.unreachable || f.depth() != 0 || f.moduleEH || f.localBase != 0 || f.interruptible || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.nLocals > 256 {
		return false, nil
	}
	for _, typ := range f.localType {
		if typ != mtI32 && typ != mtI64 {
			return false, nil
		}
	}
	look := *r
	bt, err := look.Byte()
	if err != nil || bt != 0x40 {
		return false, nil
	}
	var p integerReductionPlan
	features := shared.AMD64ModernBaseline | shared.AMD64BMI2
	if f.sc != nil {
		features = f.sc.amd64Features
	}
	if !inspectIntegerReductionLoop(look, f.localType, &p) || !features.Has(shared.AMD64SSE41) {
		return false, nil
	}
	depth := len(f.ctrl)
	if err := f.opBlockPlain(r, 0x03); err != nil {
		return true, err
	}
	var gp [2]Reg
	n := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, reg := range gpAlloc {
		if !blocked.has(reg) && f.regUser[reg] == nil {
			gp[n] = reg
			n++
			if n == len(gp) {
				break
			}
		}
	}
	if n != len(gp) {
		return true, nil
	}
	var entry [256]regionEntryLocal
	for i := 0; i < f.nLocals; i++ {
		entry[i] = regionEntryLocal{f.locals[i].reg, f.locals[i].state}
	}
	entryF := f.fpinned.union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	freeFP := 0
	for reg := Reg(0); reg < 16; reg++ {
		if !entryF.has(reg) && f.fregUser[reg] == nil {
			freeFP++
		}
	}
	if freeFP < int(p.registers) {
		f.stats.peep("integer-reduction-fp-entry-reject")
		return true, nil
	}
	reserved, pinned := f.reserved, f.pinned
	slot := f.allocSpillSlots(int(p.localN) + 2)
	off := func(i int) int32 { return f.spillOff(slot + i) }
	entryDispatch := f.a.JmpPlaceholder()
	captureEntry := f.a.Len()
	// Short trips enter the original body before any capture store. Read the
	// i32 count from its scalar header home, with no change to local ownership.
	counter := p.locals[p.counter]
	countHome := f.locals[counter.index]
	switch {
	case countHome.state == lsConstZero:
		f.a.Xor32(gp[0], gp[0])
	case countHome.reg != regNone && countHome.state != lsMem:
		f.a.MovRegReg32(gp[0], countHome.reg)
	default:
		f.loadFrameInt(gp[0], f.localAddr(int(counter.index)), mtI32)
	}
	f.a.AluRI(7, gp[0], integerReductionMinTripCount, false)
	short := f.a.JccPlaceholder(condB)
	// Capture live old versions and the trip count. allocSpillSlots raises the
	// frame high-water mark; the scalar body can reuse these same stack slots.
	// All fast-path slot accesses finish before the scalar tail can run. Do not
	// add a slot read after that tail without a separate reservation contract.
	for i, l := range p.locals[:p.localN] {
		if !p.live[l.initial] && uint8(i) != p.counter {
			continue
		}
		d := f.locals[l.index]
		w := p.nodes[l.initial].typ == mtI64
		switch {
		case d.state == lsConstZero:
			f.a.Xor32(gp[1], gp[1])
		case d.reg != regNone && d.state != lsMem:
			if w {
				f.a.MovReg64(gp[1], d.reg)
			} else {
				f.a.MovRegReg32(gp[1], d.reg)
			}
		default:
			f.loadFrameInt(gp[1], f.localAddr(int(l.index)), p.nodes[l.initial].typ)
		}
		f.a.Store64(RSP, off(i), gp[1])
	}
	toFast := f.a.JmpPlaceholder()
	fallback := f.a.Len()
	f.a.PatchRel32(short, fallback)
	f.ctrl[len(f.ctrl)-1].controlSite = fallback
	if err := f.bodyLoop(r, depth); err != nil {
		return true, err
	}
	toDone := f.a.JmpPlaceholder()
	f.a.PatchRel32(toFast, f.a.Len())
	valid := !f.unreachable && f.depth() == 0 && f.s.canonicalSlots && f.reserved == reserved && f.pinned == pinned
	// Fast publication establishes both frame and register copies. Local state
	// may change from zero or dirty to clean, but register identity must agree
	// with the original scalar header so its one-iteration tail can be reused.
	for i := 0; i < f.nLocals; i++ {
		if entry[i].reg != f.locals[i].reg {
			valid = false
			f.stats.peep("integer-reduction-homes-reject")
		}
	}
	if f.reserved != reserved || f.pinned != pinned {
		f.stats.peep("integer-reduction-masks-reject")
	}
	for _, reg := range gp {
		if f.regUser[reg] != nil || f.pinnedLocalMask.has(reg) {
			valid = false
		}
	}
	var fp [16]Reg
	mask := entryF.union(f.fpinned).union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	n = 0
	for reg := Reg(0); reg < 16; reg++ {
		if !mask.has(reg) && f.fregUser[reg] == nil && n < int(p.registers) {
			fp[n] = reg
			n++
		}
	}
	if n != int(p.registers) {
		valid = false
		f.stats.peep("integer-reduction-fp-reject")
	}
	if valid {
		// The native finalizer can remove this jump to its own fall-through.
		f.a.PatchRel32(entryDispatch, captureEntry)
		f.cpuHas(shared.AMD64SSE41)
		f.emitIntegerReduction(&p, gp, &fp, &entry, slot, fallback)
		f.stats.peep("integer-reduction-loop")
	} else {
		// Late home changes still need the reserved frame extent, but the
		// rejected path must not execute its unused capture stores or guards.
		f.a.PatchRel32(entryDispatch, fallback)
		f.a.JmpBack(fallback)
		f.stats.peep("integer-reduction-exit-reject")
	}
	f.a.PatchRel32(toDone, f.a.Len())
	return true, nil
}

func (f *fn) emitIntegerReduction(p *integerReductionPlan, gp [2]Reg, fp *[16]Reg, entry *[256]regionEntryLocal, slot, fallback int) {
	off := func(i int) int32 { return f.spillOff(slot + i) }
	reg := func(id uint8) Reg { return fp[p.register[id]] }
	tmp := fp[p.registers-1]
	// An i32 map has the layout [x0, 0, x1, 0]. This gives exact unsigned
	// widening and lets PMULUDQ form each full 32-by-32 i64 product.
	for id := uint8(1); id <= p.nodeN; id++ {
		if !p.live[id] {
			continue
		}
		n := p.nodes[id]
		switch n.op {
		case 0x20:
			i := int(n.bits)
			l := p.locals[i]
			f.loadFrameInt(gp[1], off(i), n.typ)
			f.a.MovGprToXmm(reg(id), gp[1], n.typ == mtI64)
			if l.reduction {
				continue
			} // Initial sum occupies only lane zero.
			if l.recurrence {
				f.a.AluRI(aluTable[opAdd].digit, gp[1], int32(l.step), false)
				f.a.MovGprToXmm(tmp, gp[1], false)
				f.a.SseRR(0x66, 0x6c, reg(id), tmp, false) // PUNPCKLQDQ
			} else {
				f.a.SseRR(0x66, 0x6c, reg(id), reg(id), false)
			}
		case 0x41, 0x42:
			if n.typ == mtI64 {
				f.a.MovImm64(gp[1], n.bits)
			} else {
				f.a.MovImm32(gp[1], int32(n.bits))
			}
			f.a.MovGprToXmm(reg(id), gp[1], n.typ == mtI64)
			f.a.SseRR(0x66, 0x6c, reg(id), reg(id), false)
		}
	}
	f.a.Load32(gp[0], RSP, off(int(p.counter)))
	f.a.ShiftImm(5, gp[0], 1, false)
	f.a.AlignLoop()
	top := f.a.Len()
	for id := uint8(1); id <= p.nodeN; id++ {
		if !p.live[id] {
			continue
		}
		n := p.nodes[id]
		if n.op == 0x20 || n.op == 0x41 || n.op == 0x42 {
			continue
		}
		out, left := reg(id), reg(n.left)
		switch n.op {
		case 0xac, 0xad:
			f.mov128(out, left)
			if n.op == 0xac {
				opVPsradImm.emit(f, tmp, left, 31)
				opVPsllqImm.emit(f, tmp, tmp, 32)
				opVPor.emit(f, out, out, tmp)
			}
		case 0x74, 0x75, 0x76:
			shift := opVPslldImm
			if n.op == 0x75 {
				shift = opVPsradImm
			} else if n.op == 0x76 {
				shift = opVPsrldImm
			}
			shift.emit(f, out, left, byte(p.nodes[n.right].bits)&31)
			// Arithmetic dword shifts leave the upper dwords zero because their
			// inputs are zero; signed extension happens only at extend_i32_s.
		default:
			var op simdBinaryOp
			switch n.op {
			case 0x6a:
				op = opVPaddd
			case 0x6b:
				op = opVPsubd
			case 0x6c:
				op = opVPmulld
			case 0x71:
				op = opVPand
			case 0x72:
				op = opVPor
			case 0x73:
				op = opVPxor
			case 0x7c:
				op = opVPaddq
			case 0x7e:
				op = opVPmuludq
			}
			right := reg(n.right)
			// A reduction's old sum may be the right operand. All such adds are
			// commutative; put its destination on the left to avoid the legacy
			// SIMD alias-preservation spill and keep private entry slots intact.
			if out == right && out != left {
				left, right = right, left
			}
			op.emit(f, out, left, right)
		}
	}
	// Advance both induction lanes by two scalar steps. The final version
	// already contains one step, so add one more after copying it to its home.
	for i, l := range p.locals[:p.localN] {
		if !l.recurrence || uint8(i) == p.counter {
			continue
		}
		f.mov128(reg(l.initial), reg(l.value))
		n := p.nodes[l.value]
		step := n.right
		if step == l.initial {
			step = n.left
		}
		opVPaddd.emit(f, reg(l.initial), reg(l.initial), reg(step))
	}
	f.a.AluRI(aluTable[opSub].digit, gp[0], 1, false)
	again := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(again, top)
	// Publish every scalar final version. Maps select the second scalar lane;
	// sums combine both accumulators once, with the initial value added once.
	var written [256]bool
	for i, l := range p.locals[:p.localN] {
		if !l.written {
			continue
		}
		written[l.index] = true
		typ := p.nodes[l.initial].typ
		if uint8(i) == p.counter {
			f.a.Load32(gp[1], RSP, off(i))
			f.a.AluRI(aluTable[opAnd].digit, gp[1], 1, false)
		} else if l.reduction {
			opVPsrlqImm.emit(f, tmp, reg(l.value), 0)    // Copy without changing the accumulator.
			f.a.SseMapRRI(0x66, 0, 0x73, Reg(3), tmp, 8) // PSRLDQ
			opVPaddq.emit(f, tmp, tmp, reg(l.value))
			f.a.MovXmmToGpr(gp[1], tmp, true)
		} else {
			f.mov128StoreDisp(RSP, off(int(p.localN)), reg(l.value))
			f.loadFrameInt(gp[1], off(int(p.localN))+8, typ)
		}
		d := f.locals[l.index]
		f.storeFrameInt(f.localAddr(int(l.index)), gp[1], typ)
		if d.reg != regNone {
			if typ == mtI64 {
				f.a.MovReg64(d.reg, gp[1])
			} else {
				f.a.MovRegReg32(d.reg, gp[1])
			}
		}
	}
	// The scalar compiler may clean or restore an unchanged pin at loop exit.
	// Establish the same value in both homes without changing its metadata.
	for i := 0; i < f.nLocals; i++ {
		if written[i] {
			continue
		}
		old, d := entry[i], f.locals[i]
		if old.state == d.state {
			continue
		}
		typ := f.localType[i]
		switch {
		case old.state == lsConstZero:
			f.a.Xor32(gp[1], gp[1])
		case old.reg != regNone && old.state != lsMem:
			f.moveInt(gp[1], old.reg, typ)
		default:
			f.loadFrameInt(gp[1], f.localAddr(i), typ)
		}
		f.storeFrameInt(f.localAddr(i), gp[1], typ)
		if d.reg != regNone {
			f.moveInt(d.reg, gp[1], typ)
		}
	}
	// Reuse the original body for one odd remainder. Its local homes were
	// proved identical at compile time, and its original trap behavior remains.
	f.a.Load32(gp[1], RSP, off(int(p.counter)))
	f.a.TestImm(gp[1], 1, false)
	tail := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(tail, fallback)
}
