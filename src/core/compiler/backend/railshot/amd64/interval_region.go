//go:build amd64

package amd64

const (
	minIntervalRegionBody   = 128
	minIntervalRegionLocals = 16
	maxIntervalRegionBody   = 16 << 10
	maxIntervalRegionLocals = 256
	maxIntervalRegionRegs   = 9
	maxIntervalNextUseOps   = 64
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
	// First prefer a value whose next bounded access overwrites it. This is the
	// exact dead-value case: with no pending borrowed read, the dirty register can
	// be released without publishing its old value to the frame.
	overwrites := f.intervalOverwriteBeforeRead()
	for reg, x := range f.intervalOwner {
		if x < 0 || !overwrites.has(Reg(reg)) || avoid.has(Reg(reg)) || f.pinned.has(Reg(reg)) ||
			f.intervalLocalHasMemBorrow(x) || f.intervalLocalHasValueBorrow(x) || int(f.intervalScore[x]) >= scoreLimit {
			continue
		}
		return f.releaseIntervalLocal(x, false)
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
	return f.releaseIntervalLocal(best, true)
}

func (f *fn) releaseIntervalLocal(x int, storeDirty bool) Reg {
	reg := f.locals[x].reg
	if storeDirty && f.locals[x].state == lsReg {
		f.storeFrameInt(f.localAddr(x), reg, f.localType[x])
	}
	f.demoteIntervalLocalRefs(x)
	f.locals[x].reg = regNone
	f.locals[x].state = lsMem
	f.intervalOwner[reg] = -1
	f.pinnedLocalMask = f.pinnedLocalMask.remove(reg)
	if storeDirty {
		f.stats.peep("interval-region-evict")
	} else {
		f.stats.peep("interval-region-dead-evict")
	}
	return reg
}

// intervalOverwriteBeforeRead scans at most 64 operations from the active
// forward reader. Structured boundaries and malformed immediates abandon the
// proof; unresolved locals retain the ordinary score-based eviction fallback.
func (f *fn) intervalOverwriteBeforeRead() regMask {
	var unresolved regMask
	for reg, x := range f.intervalOwner {
		if x >= 0 {
			unresolved = unresolved.add(Reg(reg))
		}
	}
	peek := f.bodyReader
	var overwritten regMask
	for fuel := 0; fuel < maxIntervalNextUseOps && unresolved != 0; fuel++ {
		op, err := peek.Byte()
		if err != nil {
			return overwritten
		}
		switch op {
		case 0x00, 0x02, 0x03, 0x04, 0x05, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f:
			return overwritten
		case 0x20, 0x21, 0x22:
			x32, err := peek.U32()
			if err != nil {
				return overwritten
			}
			x := int(x32) + f.localBase
			for reg, owner := range f.intervalOwner {
				r := Reg(reg)
				if owner != x || !unresolved.has(r) {
					continue
				}
				if op != 0x20 {
					overwritten = overwritten.add(r)
				}
				unresolved = unresolved.remove(r)
				break
			}
		default:
			if err := skipImmediates(&peek, op); err != nil {
				return overwritten
			}
		}
	}
	return overwritten
}

func (f *fn) intervalLocalHasMemBorrow(x int) bool {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stMemRef && e.st.memBorrow() == x {
			return true
		}
	}
	return false
}

func (f *fn) intervalLocalHasValueBorrow(x int) bool {
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stLocalReg && e.st.idx == x {
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
