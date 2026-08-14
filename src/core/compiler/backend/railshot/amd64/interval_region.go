//go:build amd64

package amd64

const (
	minIntervalRegionBody   = 128
	minIntervalRegionLocals = 16
	maxIntervalRegionBody   = 16 << 10
	maxIntervalRegionLocals = 256
	maxIntervalRegionRegs   = 9
)

var intervalRegionOrder = [...]Reg{R12, R13, R14, R15, R9, R10, R11, RBP, RDI, RSI}

func intervalRegionHintStorageEligible(bodyLen, nLocals int, moduleEH bool) bool {
	return intervalRegionPinsEnabled && !moduleEH &&
		bodyLen >= minIntervalRegionBody && bodyLen <= maxIntervalRegionBody &&
		nLocals >= minIntervalRegionLocals && nLocals <= maxIntervalRegionLocals
}

// prepareIntervalRegion discovers profitable integer local lifetimes in one
// call-free straight-line body. Storage is worker scratch and capped by
// locals/body size; unsupported shapes keep the existing lowering.
func (f *fn) prepareIntervalRegion(body []byte, hints funcHints) bool {
	if !intervalRegionHintStorageEligible(len(body), f.nLocals, f.moduleEH) ||
		len(hints.localScore) != f.nLocals || len(hints.localLastGet) != f.nLocals {
		return false
	}

	assigned := resizeRegScratch(f.tmpIntervalReg, f.nLocals)
	f.tmpIntervalReg = assigned
	kept := 0
	for x := 0; x < f.nLocals; x++ {
		if (f.localType[x] == mtI32 || f.localType[x] == mtI64) && hints.localLastGet[x] != 0 && hints.localScore[x] >= 2 {
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
	return true
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
	return f.evictIntervalLocalBelow(0, int(f.intervalScore[x]))
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
		s := 0
		if x < len(f.intervalScore) {
			s = int(f.intervalScore[x])
		}
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
		if e.kind == ekValue && e.st.kind == stLocalReg && e.st.idx == x {
			e.st.kind = stLocalRef
			e.st.reg = regNone
		}
	}
}
