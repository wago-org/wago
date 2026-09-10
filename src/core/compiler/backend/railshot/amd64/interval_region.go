//go:build amd64

package amd64

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
		!f.moduleHasSIMD && !hints.flags.has(hintHasCall|hintHasControlFlow|hintUsesBulkMem) && len(f.ft.Results) <= 1
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
	f.pinnedLocalMask = f.pinnedLocalMask.add(reg)
	f.stats.peep("interval-region-reactivate")
	f.noteResidencyActivation(load)
}

func (f *fn) claimIntervalReg(x int) Reg {
	active := 0
	for _, owner := range f.intervalOwner {
		if owner >= 0 {
			active++
		}
	}
	if active < f.intervalRegLimit {
		baseLimit := intervalRegionRegLimit(f.guardMode)
		for _, reg := range intervalRegionOrder[:baseLimit] {
			if !f.reserved.has(reg) && !f.pinned.has(reg) && !f.pinnedLocalMask.has(reg) &&
				f.regUser[reg] == nil && f.intervalOwner[reg] < 0 {
				return reg
			}
		}
		if f.intervalScratch {
			// Only RDX passed the semantic corpus. RAX and multi-scratch variants
			// conflict with implicit arithmetic lowering even in this bounded class.
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
	return f.evictIntervalLocalBelow(0, f.intervalResidencyScore(x))
}

func (f *fn) intervalResidencyScore(x int) int {
	if x < 0 || x >= len(f.intervalScore) {
		return 0
	}
	score := int(localHotness(f.intervalScore[x]))
	if f.opt(optIntervalI64Weight) && x < len(f.localType) && f.localType[x] == mtI64 {
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
	for reg, x := range f.intervalOwner {
		if x < 0 || avoid.has(Reg(reg)) || f.pinned.has(Reg(reg)) || f.intervalLocalHasMemBorrow(x) {
			continue
		}
		s := f.intervalResidencyScore(x)
		if s < scoreLimit && s < bestScore {
			best, bestScore = x, s
		}
	}
	if best < 0 {
		return regNone
	}
	reg := f.locals[best].reg
	if f.locals[best].state == lsReg {
		f.storeFrameInt(f.localAddr(best), reg, f.localType[best])
		if f.stats != nil {
			f.stats.Residency.DirtyWritebacks++
		}
	}
	f.demoteIntervalLocalRefs(best)
	f.locals[best].reg = regNone
	f.locals[best].state = lsMem
	f.intervalOwner[reg] = -1
	f.pinnedLocalMask = f.pinnedLocalMask.remove(reg)
	f.stats.peep("interval-region-evict")
	if f.stats != nil {
		f.stats.Residency.Evictions++
	}
	return reg
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
	active := 0
	for _, owner := range f.intervalOwner {
		if owner >= 0 {
			active++
		}
	}
	if active > r.MaxActive {
		r.MaxActive = active
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

func (f *fn) demoteIntervalLocalRefs(x int) {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.isValue() && e.st.kind == stLocalReg && e.st.idx == uint32(x) {
			e.st.kind = stLocalRef
			e.st.reg = regNone
		}
	}
}
