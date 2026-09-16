//go:build amd64

package amd64

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
	minIntervalRegionBody   = 128
	minIntervalRegionLocals = 16
	maxIntervalRegionBody   = 16 << 10
	maxIntervalRegionLocals = 256
	maxIntervalRegionRegs   = 10
)

// Keep R8 last: unlike RAX/RCX/RDX it has no unavoidable arithmetic role, but
// its encoding costs a REX prefix and bulk-memory/call lowering uses it as fixed
// scratch. Regional functions exclude those boundaries before this pool is used.
var intervalRegionOrder = [...]Reg{R12, R13, R14, R15, R9, R10, R11, RBP, RDI, RSI, R8}

func intervalRegionRegLimit(guardMode bool) int {
	if guardMode {
		// Signals-based bounds lowering uses R8 as fixed scratch even in a
		// call-free region. Keep it out of the regional lease pool.
		return maxIntervalRegionRegs - 1
	}
	return maxIntervalRegionRegs
}

func intervalRegionHintStorageEligible(enabled bool, bodyLen, nLocals int, moduleEH bool) bool {
	return enabled && !moduleEH &&
		bodyLen >= minIntervalRegionBody && bodyLen <= maxIntervalRegionBody &&
		nLocals >= minIntervalRegionLocals && nLocals <= maxIntervalRegionLocals
}

// intervalScratchLeaseEligible preserves a transient-register floor when
// module-wide roles reserve registers from the regional pool. Leasing RDX with
// two or more unavailable base registers can otherwise leave every allocatable
// GPR occupied by regional locals, fixed operands, or module state.
func intervalScratchLeaseEligible(reserved regMask, guardMode bool) bool {
	baseLimit := intervalRegionRegLimit(guardMode)
	unavailable := 0
	for _, reg := range intervalRegionOrder[:baseLimit] {
		if reserved.has(reg) {
			unavailable++
		}
	}
	return unavailable < 2
}

// prepareIntervalRegion discovers profitable integer local lifetimes in one
// call-free straight-line body. Storage is worker scratch and capped by
// locals/body size; unsupported shapes keep the existing lowering.
func (f *fn) prepareIntervalRegion(body []byte, hints *funcHintView) bool {
	if !intervalRegionHintStorageEligible(f.opt(optIntervalRegionPins), len(body), f.nLocals, f.moduleEH) ||
		len(hints.localScore) != f.nLocals || len(hints.localLastGet) != f.nLocals {
		return false
	}
	f.intervalRegLimit = intervalRegionRegLimit(f.guardMode)
	// SIMD lowering has fixed integer scratch uses which are not all represented
	// by the scalar fixed-scratch scan. Keep RDX available throughout a SIMD
	// module: scalar helper functions share its module register/pinning plan, and
	// the full Blake oracle reaches the high-pressure overlap there.
	f.intervalScratch = f.opt(optIntervalScratchLease) && !hints.hasFixedScratchLease() &&
		!f.moduleHasSIMD && !hints.flags.has(hintHasCall|hintHasControlFlow|hintUsesBulkMem) && len(f.ft.Results) <= 1 &&
		intervalScratchLeaseEligible(f.reserved, f.guardMode)
	if f.intervalScratch {
		f.intervalRegLimit++
	}
	mt0, hasMemory := f.m.MemoryType(0)
	strictScratchLease := f.guardMode && !f.moduleHasSIMD && !f.threadedMemory0 &&
		f.m.TableCount() == 0 && f.m.MemCount() <= 1 && (!hasMemory || !mt0.Limits.Addr64) &&
		len(f.gcTypeLayouts) == 0 && len(f.customInstructions) == 0 &&
		!hints.flags.has(hintHasCall|hintHasControlFlow|hintUsesBulkMem)
	f.intervalR8 = f.opt(optIntervalR8Lease) && strictScratchLease
	if f.intervalR8 {
		f.intervalRegLimit++
	}

	assigned := resizeRegScratch(f.tmpIntervalReg, f.nLocals)
	f.tmpIntervalReg = assigned
	kept := 0
	for x := 0; x < f.nLocals; x++ {
		if (f.localType[x] == mtI32 || f.localType[x] == mtI64) && hints.localLastGet[x] != 0 && localHotness(hints.localScore[x]) >= 2 {
			assigned[x] = RSP // compact eligibility marker; RSP is never allocatable.
			kept++
		}
	}
	if kept == 0 {
		return false
	}
	f.intervalReg, f.intervalLast, f.intervalScore = assigned, hints.localLastGet, hints.localScore
	f.intervalNext = f.opt(optIntervalNextUse)
	f.intervalI64Weight = f.opt(optIntervalI64Weight)
	if f.intervalNext {
		f.prepareIntervalEvents(body, hints.localEventCount())
	}
	for i := range f.intervalOwner {
		f.intervalOwner[i] = -1
	}
	f.stats.peep("interval-region")
	if f.intervalScratch {
		f.stats.peep("interval-scratch-lease")
	}
	if f.intervalR8 {
		f.stats.peep("interval-r8-lease")
	}
	f.noteResidencyCandidates(kept)
	return true
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

func resizeRegScratch(buf []Reg, n int) []Reg {
	if cap(buf) < n {
		buf = make([]Reg, n)
	} else {
		buf = buf[:n]
	}
	for i := range buf {
		buf[i] = regNone
	}
	return buf
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

// prepareIntervalEvents builds one intrusive, source-ordered event list per
// local during bounded-function setup. Eviction queries then move a monotonic
// cursor instead of repeatedly decoding the remainder of the body.
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
		var x int
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
		index, err := r.U32()
		if err != nil {
			events = events[:0]
			break
		}
		x = int(index)
		if x < 0 || x >= f.nLocals {
			events = events[:0]
			break
		}
		if f.intervalReg[x] == regNone {
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

// activateIntervalLocal opportunistically restores an assigned regional local
// after pressure evicted it. A busy register is left to the ordinary lowering;
// forcing a spill merely to recreate the cache loses the cache's purpose.
func (f *fn) activateIntervalLocal(x, pos int, load bool) {
	if x < 0 || x >= len(f.intervalReg) || f.intervalReg[x] == regNone ||
		uint32(pos) > f.intervalLast[x] || f.locals[x].reg != regNone {
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
		f.loadFrameInt(reg, f.localAddr(x), f.localType[x])
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
		baseLimit := intervalRegionRegLimit(f.guardMode)
		for _, reg := range intervalRegionOrder[:baseLimit] {
			if !f.reserved.has(reg) && !f.pinned.has(reg) && !f.pinnedLocalMask.has(reg) &&
				f.regUser[reg] == nil && f.intervalOwner[reg] < 0 {
				return reg
			}
		}
		if f.intervalScratch {
			// RDX has no implicit role in the admitted instruction class. RAX and
			// multi-scratch variants conflict with implicit arithmetic lowering.
			if reg := RDX; !f.reserved.has(reg) && !f.pinned.has(reg) && !f.pinnedLocalMask.has(reg) &&
				f.regUser[reg] == nil && f.intervalOwner[reg] < 0 {
				return reg
			}
		}
		if f.intervalR8 {
			if reg := R8; !f.reserved.has(reg) && !f.pinned.has(reg) && !f.pinnedLocalMask.has(reg) &&
				f.regUser[reg] == nil && f.intervalOwner[reg] < 0 {
				return reg
			}
		}
	}
	scoreLimit := f.intervalResidencyScore(x)
	if f.nextUsePolicy() {
		scoreLimit = int(^uint(0) >> 1)
	}
	return f.evictIntervalLocalBelow(0, scoreLimit)
}

func (f *fn) intervalResidencyScore(x int) int {
	if x < 0 || x >= len(f.intervalScore) {
		return 0
	}
	score := int(localHotness(f.intervalScore[x]))
	if f.intervalI64Weight && x < len(f.localType) && f.localType[x] == mtI64 {
		score += score / 2
	}
	return score
}

// takeFinalIntervalGet transfers a dying local's register directly to the
// operand stack. Older borrowed references are realized first; no copy or frame
// access is needed for the final get itself.
func (f *fn) takeFinalIntervalGet(x, pos int) (Reg, bool) {
	if x < 0 || x >= len(f.intervalReg) || f.intervalReg[x] == regNone ||
		f.intervalLast[x] != uint32(pos) || f.locals[x].reg == regNone {
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

// evictIntervalLocal turns one active regional pin back into its canonical frame
// value when expression pressure needs the register. Borrowed local.get leaves
// become lazy frame reads, so a wide Valent tree can reclaim a cache register
// without first allocating a copy register. A pending memory load whose address
// borrows the local remains a hard blocker because its effective address must
// stay live until that load is emitted.
func (f *fn) evictIntervalLocal(avoid regMask) Reg {
	return f.evictIntervalLocalBelow(avoid, int(^uint(0)>>1))
}

func (f *fn) evictIntervalLocalBelow(avoid regMask, scoreLimit int) Reg {
	if len(f.intervalReg) == 0 {
		return regNone
	}
	best, bestScore := -1, int(^uint(0)>>1)
	bestNext, bestDead := uint32(0), false
	borrowed := f.intervalBorrowedRegs()
	for reg, x := range f.intervalOwner {
		if x < 0 || avoid.has(Reg(reg)) || f.pinned.has(Reg(reg)) || borrowed.has(Reg(reg)) {
			continue
		}
		s := f.intervalResidencyScore(x)
		if s >= scoreLimit {
			continue
		}
		if f.nextUsePolicy() {
			next, dead := f.nextIntervalLocalAccess(x)
			if best < 0 || dead && !bestDead || dead == bestDead && next > bestNext ||
				dead == bestDead && next == bestNext && s < bestScore {
				best, bestScore, bestNext, bestDead = x, s, next, dead
			}
		} else if s < bestScore {
			best, bestScore = x, s
		}
	}
	if best < 0 {
		return regNone
	}
	reg := f.locals[best].reg
	if f.locals[best].state == lsReg && !bestDead {
		f.storeFrameInt(f.localAddr(best), reg, f.localType[best])
		if f.stats != nil {
			f.stats.Residency.DirtyWritebacks++
		}
	}
	if bestDead {
		f.stats.peep("interval-dead-store-elide")
	}
	// Exact next-use selection excludes every resident local referenced anywhere
	// in the pending expression forest, so there is nothing left to demote. The
	// legacy hotness policy only blocks address borrows and still needs to rewrite
	// lazy direct references before releasing their carrier.
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

// nextIntervalLocalAccess returns the next source-order access to x after the
// instruction currently being lowered. A definition before any read kills the
// resident version, so its dirty frame store is dead. Callers first exclude all
// locals borrowed by pending deferred expressions; source order is therefore a
// sound approximation of dynamic next use in this straight-line-only region.
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

func (f *fn) intervalLocalHasMemBorrow(x int) bool {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.isValue() && e.st.kind == stMemRef && e.st.memBorrow() == x {
			return true
		}
	}
	return false
}

func (f *fn) intervalLocalBorrowed(x int) bool {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if subtreeRefsLocal(e, x) || subtreeBorrowsLocalAddress(e, x) {
			return true
		}
	}
	return false
}

// intervalBorrowedRegs finds every resident local referenced by the pending
// expression forest in one traversal. Victim selection used to rescan the whole
// forest once per resident register, making exact next-use compilation
// quadratic in both expression depth and cache occupancy.
func (f *fn) intervalBorrowedRegs() regMask {
	var borrowed regMask
	var visit func(*elem)
	visit = func(e *elem) {
		if e == nil {
			return
		}
		if e.isDeferred() {
			visit(e.arg0)
			visit(e.arg1)
			return
		}
		if !e.isValue() {
			return
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
	for e := f.s.head.next; e != f.s.head; e = e.next {
		visit(e)
	}
	return borrowed
}

func (f *fn) demoteIntervalLocalRefs(x int) {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.isValue() && e.st.kind == stLocalReg && e.st.idx == uint32(x) {
			e.st.kind = stLocalRef
			e.st.reg = regNone
		}
	}
}
