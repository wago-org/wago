//go:build arm64

package arm64

const (
	minIntervalRegionBody   = 128
	minIntervalRegionLocals = 32
	maxIntervalRegionBody   = 16 << 10
	maxIntervalRegionLocals = 256
	maxIntervalRegionRegs   = 19
)

func intervalRegionRegLimit(x27Reserved bool) int {
	// Caching the explicit-bounds memory size reserves X27, so nineteen active
	// leases would leave only two registers from the scratch-capable tail.
	if x27Reserved {
		return maxIntervalRegionRegs - 1
	}
	return maxIntervalRegionRegs
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
	f.intervalRegLimit = intervalRegionRegLimit(f.memSizeReg == X27)
	for i := range f.intervalOwner {
		f.intervalOwner[i] = -1
	}
	f.stats.peep("interval-region")
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
		for _, reg := range intervalRegionOrder {
			if !f.reserved.has(reg) && !f.pinned.has(reg) && !f.pinnedLocalMask.has(reg) &&
				f.regUser[reg] == nil && f.intervalOwner[reg] < 0 {
				return reg
			}
		}
		return regNone
	}
	return f.evictIntervalLocalBelow(0, int(localHotness(f.intervalScore[x])))
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
	for reg, x := range f.intervalOwner {
		if x < 0 || avoid.has(Reg(reg)) || f.pinned.has(Reg(reg)) || f.intervalLocalHasMemBorrow(x) {
			continue
		}
		score := int(localHotness(f.intervalScore[x]))
		if score < scoreLimit && score < bestScore {
			best, bestScore = x, score
		}
	}
	if best < 0 {
		return regNone
	}
	reg := f.locals[best].reg
	if f.locals[best].state == lsReg {
		f.st64(SP, f.localOff(best), reg)
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
