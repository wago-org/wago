//go:build arm64

package arm64

// loopCounterEffects supplements the pure integer decoder with branch reads
// and unsigned division. Branch ownership is proved by the unroller first.
func loopCounterEffects(word uint32) (guardedIntegerEffects, bool) {
	if _, branch := branchTarget(0, word); branch {
		if word&0xfc000000 == 0x14000000 {
			return guardedIntegerEffects{}, true
		}
		if word&0xff000010 == 0x54000000 {
			return guardedIntegerEffects{readsFlags: true}, true
		}
		if word&0x7e000000 == 0x34000000 {
			return guardedIntegerEffects{reads: (1 << (word & 31)) & 0x7fffffff}, true
		}
		return guardedIntegerEffects{}, false
	}
	if word&0x7fe0fc00 == 0x1ac00800 {
		return guardedIntegerEffects{reads: ((1 << (word >> 5 & 31)) | (1 << (word >> 16 & 31))) & 0x7fffffff, writes: (1 << (word & 31)) & 0x7fffffff}, true
	}
	return guardedIntegerWord(word)
}

func planLoopCounterBase(words []uint32, exits, traps uint64, pinned, free regMask) (Reg, Reg, bool) {
	n := len(words)
	if n < 9 || n > 64 {
		return regNone, regNone, false
	}
	tail := words[n-1]
	d := Reg(tail & 31)
	if tail&0x7fc00000 != 0x11000000 || tail>>10&0xfff != 1 || d != Reg(tail>>5&31) || d >= 31 || !pinned.has(d) {
		return regNone, regNone, false
	}
	var used uint32
	for i, word := range words {
		e, ok := loopCounterEffects(word)
		if !ok || i < n-1 && e.writes&(1<<d) != 0 {
			return regNone, regNone, false
		}
		used |= e.reads | e.writes
		if target, branch := branchTarget(4*i, word); branch {
			if (exits|traps)&(uint64(1)<<i) != 0 {
				mask := uint32(0x00ffffe0)
				if word&0xfc000000 == 0x14000000 {
					if traps&(uint64(1)<<i) != 0 {
						return regNone, regNone, false
					}
					mask = 0x03ffffff
				}
				if word&mask != 0 {
					return regNone, regNone, false
				}
			} else if target <= 4*i || target > 4*(n-1) {
				return regNone, regNone, false
			}
		} else if (exits|traps)&(uint64(1)<<i) != 0 {
			return regNone, regNone, false
		}
	}
	for _, aux := range [...]Reg{X11, X12, X13, X14, X15} {
		if free.has(aux) && used&(1<<aux) == 0 {
			return d, aux, true
		}
	}
	return regNone, regNone, false
}

// prepareLoopCounterBase uses existing loop-entry alignment space to snapshot
// the counter once per group. Each copied tail derives d=base+phase directly,
// removing the chain of four increments while keeping the architectural local
// correct at every exit and trap. Code size and all original site offsets stay
// unchanged. Unsupported entry shapes retain ordinary copied increments.
func (f *fn) prepareLoopCounterBase(fr *ctrlFrame, words []uint32, owners []*ctrlFrame, traps uint64) bool {
	if f.loopSeedSite != fr.controlSite || fr.controlSite < 4 || rdWord(f.a.B, fr.controlSite-4) != nopWord || fr.height != 0 || len(f.intervalLast) != 0 || f.intervalControl {
		return false
	}
	blocked := f.reserved.union(f.pinned).union(f.pinnedLocalMask)
	var free regMask
	for _, r := range [...]Reg{X11, X12, X13, X14, X15} {
		if !blocked.has(r) && f.regUser[r] == nil && r != f.globalCellReg && r != f.memSizeReg && r != f.memLimitReg && r != f.trapCellReg {
			free = free.add(r)
		}
	}
	var exits uint64
	for i, owner := range owners {
		if owner != nil {
			exits |= uint64(1) << i
		}
	}
	d, aux, ok := planLoopCounterBase(words, exits, traps, f.pinnedLocalMask, free)
	if !ok {
		return false
	}
	sf := words[len(words)-1] & 0x80000000
	wrWord(f.a.B, fr.controlSite-4, sf|0x2a0003e0|uint32(d)<<16|uint32(aux))
	words[len(words)-1] = sf | 0x11000400 | uint32(aux)<<5 | uint32(d)
	wrWord(f.a.B, f.a.Len()-4, words[len(words)-1])
	f.stats.peep("scalar-loop-counter-base")
	return true
}
