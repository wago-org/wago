//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

const (
	noIntervalEvent   = ^uint32(0)
	intervalEventKill = uint32(1 << 31)
)

type intervalLocalEvent struct {
	pos  uint32
	next uint32
}

const (
	minIntervalRegionBody        = 128
	minIntervalRegionLocals      = 32
	maxIntervalRegionBody        = 16 << 10
	maxIntervalRegionLocals      = 256
	maxIntervalRegionRegs        = 19
	intervalRegionTransientFloor = 3
)

func intervalRegionRegLimit(reserved regMask) int {
	available := 0
	for _, reg := range intervalRegionOrder {
		if !reserved.has(reg) {
			available++
		}
	}
	limit := available - intervalRegionTransientFloor
	if limit < 0 {
		return 0
	}
	if limit > maxIntervalRegionRegs {
		return maxIntervalRegionRegs
	}
	return limit
}

var intervalRegionOrder = [...]Reg{
	X19, X20, X21, X22, X23, X24, X25, X27,
	X9, X10, X11, X12, X13, X14, X15, X8,
	X7, X6, X5, X4, X3, X2,
}

func intervalRegionHintStorageEligible(enabled bool, bodyLen, nLocals int, moduleEH bool) bool {
	return enabled && !moduleEH &&
		bodyLen >= minIntervalRegionBody && bodyLen <= maxIntervalRegionBody &&
		nLocals >= minIntervalRegionLocals && nLocals <= maxIntervalRegionLocals
}

// prepareIntervalRegion discovers profitable integer-local lifetimes in one
// call-free straight-line body. Storage is worker scratch and capped by body and
// local counts; unsupported shapes keep the existing whole-function allocator.
func (f *fn) prepareIntervalRegion(body []byte, hints *funcHintView) bool {
	if !intervalRegionHintStorageEligible(f.opt(optIntervalRegionPins), len(body), f.nLocals, f.moduleEH) ||
		len(hints.localScore) != f.nLocals || len(hints.localLastGet) != f.nLocals {
		return false
	}

	kept := 0
	for x := 0; x < f.nLocals; x++ {
		if (f.localType[x] == mtI32 || f.localType[x] == mtI64) && hints.localLastGet[x] != 0 && localHotness(hints.localScore[x]) >= 2 {
			kept++
		}
	}
	if kept == 0 {
		return false
	}
	f.intervalLast, f.intervalScore = hints.localLastGet, hints.localScore
	f.intervalNext = f.opt(optIntervalNextUse) && !hints.flags.has(hintModuleSIMD)
	if f.intervalNext {
		f.prepareIntervalEvents(body, hints.localEventCount())
	}
	f.intervalRegLimit = intervalRegionRegLimit(f.reserved)
	for i := range f.intervalOwner {
		f.intervalOwner[i] = -1
	}
	f.stats.peep("interval-region")
	f.noteResidencyCandidates(kept)
	return true
}

func resizeIntervalIndexScratch(buf []uint32, n int) []uint32 {
	if cap(buf) < n {
		buf = make([]uint32, n)
	} else {
		buf = buf[:n]
	}
	for i := range buf {
		buf[i] = noIntervalEvent
	}
	return buf
}

func (f *fn) intervalEligible(x int) bool {
	return x >= 0 && x < len(f.intervalLast) && f.intervalLast[x] != 0 &&
		localHotness(f.intervalScore[x]) >= 2 &&
		(f.localType[x] == mtI32 || f.localType[x] == mtI64)
}

// prepareIntervalEvents builds one intrusive source-ordered access list per
// eligible local. Victim selection advances a monotonic cursor instead of
// repeatedly decoding the rest of the function.
func (f *fn) prepareIntervalEvents(body []byte, reserve int) {
	events := f.tmpIntervalEvents[:0]
	if cap(events) < reserve {
		events = make([]intervalLocalEvent, 0, reserve)
	}
	index := resizeIntervalIndexScratch(f.tmpIntervalIndex, 2*f.nLocals)
	head, tail := index[:f.nLocals], index[f.nLocals:]
	r := wasm.ReaderFrom(body)
scan:
	for r.HasNext() {
		pos := uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			break
		}
		kill := false
		switch op {
		case 0x20: // local.get
		case 0x21, 0x22: // local.set / local.tee
			kill = true
		default:
			if _, ok := wasm.ImmediateFreeInstructionKind(op); ok {
				continue
			}
			var imm wasm.InstructionImmediate
			if err := f.classifier.ClassifyInto(&r, op, &imm); err != nil {
				events = events[:0]
				break scan
			}
			continue
		}
		idx, err := r.U32()
		if err != nil || uint64(idx) >= uint64(f.nLocals) {
			events = events[:0]
			break
		}
		x := int(idx)
		if !f.intervalEligible(x) {
			continue
		}
		i := uint32(len(events))
		if kill {
			pos |= intervalEventKill
		}
		events = append(events, intervalLocalEvent{pos: pos, next: noIntervalEvent})
		if tail[x] == noIntervalEvent {
			head[x] = i
		} else {
			events[tail[x]].next = i
		}
		tail[x] = i
	}
	f.tmpIntervalEvents, f.tmpIntervalIndex = events, index
	if len(events) != 0 {
		f.intervalEvents, f.intervalHead = events, head
	}
}

func (f *fn) noteResidencyEvents(hints *funcHintView) {
	if f.stats == nil {
		return
	}
	f.stats.Residency.Events = hints.localEventCount()
	f.stats.Residency.Shadow = hints.residencyShadow
	if hints.localEventOverflowed() {
		f.stats.Residency.EventOverflows = 1
	}
}

// activateIntervalLocal restores an assigned regional local when a register is
// free. A busy file is left to ordinary lowering; spilling merely to recreate
// the cache would defeat its purpose.
func (f *fn) activateIntervalLocal(x, pos int, load bool) {
	if x < 0 || x >= len(f.intervalLast) || f.intervalLast[x] == 0 || localHotness(f.intervalScore[x]) < 2 ||
		(f.localType[x] != mtI32 && f.localType[x] != mtI64) || uint32(pos) > f.intervalLast[x] || f.locals[x].reg != regNone {
		return
	}
	reg := f.claimIntervalReg(x)
	if reg == regNone {
		f.noteResidencyPressureMiss()
		return
	}
	f.invalidateGlobalsCache()
	f.invalidateStoreForward()
	if load {
		f.ld64(reg, SP, f.localOff(x))
		f.locals[x].state = lsStackReg
	}
	f.locals[x].reg = reg
	f.intervalOwner[reg] = x
	f.intervalActive++
	f.pinnedLocalMask = f.pinnedLocalMask.add(reg)
	f.stats.peep("interval-region-reactivate")
	f.noteResidencyActivation(load)
}

func (f *fn) claimIntervalReg(x int) Reg {
	if f.intervalActive < f.intervalRegLimit {
		for _, reg := range intervalRegionOrder {
			if !f.reserved.has(reg) && !f.pinned.has(reg) && !f.pinnedLocalMask.has(reg) &&
				f.regUser[reg] == nil && f.intervalOwner[reg] < 0 {
				return reg
			}
		}
		return regNone
	}
	scoreLimit := int(localHotness(f.intervalScore[x]))
	if f.nextUsePolicy() {
		scoreLimit = int(^uint(0) >> 1)
	}
	return f.evictIntervalLocalBelow(0, scoreLimit)
}

// takeFinalIntervalGet transfers a dying local's register directly to the
// operand stack. Older borrowed references are realized before ownership moves.
func (f *fn) takeFinalIntervalGet(x, pos int) (Reg, bool) {
	if x < 0 || x >= len(f.intervalLast) || f.intervalLast[x] != uint32(pos) || f.locals[x].reg == regNone {
		return regNone, false
	}
	f.realizeLocalRefs(x, nil)
	reg := f.locals[x].reg
	f.locals[x].reg = regNone
	f.locals[x].state = lsMem
	f.intervalOwner[reg] = -1
	f.intervalActive--
	f.pinnedLocalMask = f.pinnedLocalMask.remove(reg)
	if f.stats != nil {
		f.stats.Residency.FinalTransfers++
	}
	return reg, true
}

func (f *fn) evictIntervalLocal(avoid regMask) Reg {
	return f.evictIntervalLocalBelow(avoid, int(^uint(0)>>1))
}

func (f *fn) evictIntervalLocalBelow(avoid regMask, scoreLimit int) Reg {
	if len(f.intervalLast) == 0 {
		return regNone
	}
	best, bestScore := -1, int(^uint(0)>>1)
	bestNext, bestDead := uint32(0), false
	borrowed := f.intervalBorrowedRegs()
	for reg, x := range f.intervalOwner {
		if x < 0 || avoid.has(Reg(reg)) || f.pinned.has(Reg(reg)) || borrowed.has(Reg(reg)) {
			continue
		}
		score := int(localHotness(f.intervalScore[x]))
		if score >= scoreLimit {
			continue
		}
		if f.nextUsePolicy() {
			next, dead := f.nextIntervalLocalAccess(x)
			if best < 0 || dead && !bestDead || dead == bestDead && next > bestNext ||
				dead == bestDead && next == bestNext && score < bestScore {
				best, bestScore, bestNext, bestDead = x, score, next, dead
			}
		} else if score < bestScore {
			best, bestScore = x, score
		}
	}
	if best < 0 {
		return regNone
	}
	reg := f.locals[best].reg
	if f.locals[best].state == lsReg && !bestDead {
		f.st64(SP, f.localOff(best), reg)
		if f.stats != nil {
			f.stats.Residency.DirtyWritebacks++
		}
	}
	if bestDead {
		f.stats.peep("interval-dead-store-elide")
	}
	if !f.nextUsePolicy() {
		f.demoteIntervalLocalRefs(best)
	}
	f.locals[best].reg = regNone
	f.locals[best].state = lsMem
	f.intervalOwner[reg] = -1
	f.intervalActive--
	f.pinnedLocalMask = f.pinnedLocalMask.remove(reg)
	f.stats.peep("interval-region-evict")
	if f.stats != nil {
		f.stats.Residency.Evictions++
	}
	return reg
}

func (f *fn) nextUsePolicy() bool {
	return f.intervalNext && len(f.intervalEvents) != 0
}

func (f *fn) nextIntervalLocalAccess(x int) (next uint32, dead bool) {
	if f.wasmPC < f.tracePCBase || x < 0 || x >= len(f.intervalHead) {
		return 0, false
	}
	current := uint32(f.wasmPC - f.tracePCBase)
	i := f.intervalHead[x]
	for i != noIntervalEvent && i < uint32(len(f.intervalEvents)) && f.intervalEvents[i].pos&^intervalEventKill <= current {
		i = f.intervalEvents[i].next
	}
	f.intervalHead[x] = i
	if i == noIntervalEvent || i >= uint32(len(f.intervalEvents)) {
		return noIntervalEvent, true
	}
	e := f.intervalEvents[i]
	return e.pos &^ intervalEventKill, e.pos&intervalEventKill != 0
}

func (f *fn) noteResidencyCandidates(n int) {
	if f.stats != nil {
		f.stats.Residency.Candidates += n
	}
}

func (f *fn) noteResidencyPressureMiss() {
	if f.stats != nil {
		f.stats.Residency.PressureMisses++
	}
}

func (f *fn) noteResidencyActivation(load bool) {
	if f.stats == nil {
		return
	}
	r := &f.stats.Residency
	r.Activations++
	if load {
		r.ActivationLoads++
	}
	if f.intervalActive > r.MaxActive {
		r.MaxActive = f.intervalActive
	}
}

func (f *fn) intervalBorrowedRegs() regMask {
	var borrowed regMask
	for e := f.s.next(f.s.head); e != f.s.head; e = f.s.next(e) {
		if e.elemKind() != ekValue {
			continue
		}
		x := -1
		switch e.st.kind {
		case stLocalReg:
			x = e.st.index()
		case stMemRef:
			x = e.st.memBorrow()
		}
		if x >= 0 && x < len(f.locals) {
			if reg := f.locals[x].reg; reg != regNone {
				borrowed = borrowed.add(reg)
			}
		}
	}
	return borrowed
}

func (f *fn) intervalLocalHasMemBorrow(x int) bool {
	for e := f.s.next(f.s.head); e != f.s.head; e = f.s.next(e) {
		if e.elemKind() == ekValue && e.st.kind == stMemRef && e.st.memBorrow() == x {
			return true
		}
	}
	return false
}

func (f *fn) demoteIntervalLocalRefs(x int) {
	for e := f.s.next(f.s.head); e != f.s.head; e = f.s.next(e) {
		if e.elemKind() == ekValue && e.st.kind == stLocalReg && e.st.idx == uint32(x) {
			e.st.kind = stLocalRef
			e.st.reg = regNone
		}
	}
}
