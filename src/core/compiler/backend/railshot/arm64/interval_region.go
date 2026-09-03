//go:build arm64

package arm64

const (
	minIntervalRegionBody   = 128
	minIntervalRegionLocals = 32
	maxIntervalRegionBody   = 16 << 10
	maxIntervalRegionLocals = 256
	maxIntervalRegionRegs   = 18
)

var intervalRegionOrder = [...]Reg{
	X19, X20, X21, X22, X23, X24, X25, X27,
	X9, X10, X11, X12, X13, X14, X15, X8,
	X7, X6, X5, X4, X3, X2,
}

// prepareIntervalRegion discovers profitable integer-local lifetimes in one
// call-free straight-line body. Storage is worker scratch and capped by body and
// local counts; unsupported shapes keep the existing whole-function allocator.
func (f *fn) prepareIntervalRegion(body []byte, hints *funcHintView) bool {
	if !f.opt(optIntervalRegionPins) || len(body) < minIntervalRegionBody || len(body) > maxIntervalRegionBody ||
		f.nLocals < minIntervalRegionLocals || f.nLocals > maxIntervalRegionLocals || f.moduleEH ||
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
	for i := range f.intervalOwner {
		f.intervalOwner[i] = -1
	}
	f.stats.peep("interval-region")
	return true
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
}

func (f *fn) claimIntervalReg(x int) Reg {
	active := 0
	for _, owner := range f.intervalOwner {
		if owner >= 0 {
			active++
		}
	}
	if active < maxIntervalRegionRegs {
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
	}
	f.demoteIntervalLocalRefs(best)
	f.locals[best].reg = regNone
	f.locals[best].state = lsMem
	f.intervalOwner[reg] = -1
	f.pinnedLocalMask = f.pinnedLocalMask.remove(reg)
	f.stats.peep("interval-region-evict")
	return reg
}

func (f *fn) intervalLocalHasMemBorrow(x int) bool {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stMemRef && e.st.memBorrow() == x {
			return true
		}
	}
	return false
}

func (f *fn) demoteIntervalLocalRefs(x int) {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stLocalReg && e.st.idx == uint32(x) {
			e.st.kind = stLocalRef
			e.st.reg = regNone
		}
	}
}
