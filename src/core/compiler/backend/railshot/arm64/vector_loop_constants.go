//go:build arm64

package arm64

// A checked vector loop can temporarily use optional literal-cache registers.
// Regional residency, cached addresses and live register owners keep their
// existing fallback; neither branch may observe a changed cache contract.
func (f *fn) canBorrowVectorConstants(available, required int) bool {
	if available >= required {
		return true
	}
	if len(f.intervalLast) != 0 || available+int(f.iconstN) < required {
		return false
	}
	for i := uint8(0); i < f.iconstN; i++ {
		c := f.iconsts[i]
		if c.address || f.pinned.has(c.reg) || f.pinnedLocalMask.has(c.reg) || f.regUser[c.reg] != nil {
			return false
		}
	}
	return true
}

type vectorConstantCache struct {
	constants [4]intConstReg
	count     uint8
}

// Call after canonicalizing operands and locals, before emitting either arm.
func (f *fn) borrowVectorConstants() vectorConstantCache {
	saved := vectorConstantCache{f.iconsts, f.iconstN}
	f.releaseScopedLoopConsts(0)
	return saved
}

func (f *fn) restoreVectorConstants(saved vectorConstantCache) {
	if saved.count == 0 {
		return
	}
	for i := uint8(0); i < saved.count; i++ {
		c := saved.constants[i]
		f.spillIfUsed(c.reg)
		f.loadConst(c.reg, storage{kind: stConst, typ: c.typ, cval: c.bits})
		f.reserved = f.reserved.add(c.reg)
		if regallocCheckEnabled {
			f.checkImmutable(c.reg, false, 8)
		}
	}
	f.iconsts, f.iconstN = saved.constants, saved.count
	f.stats.peep("vector-loop-borrowed-constants")
}
