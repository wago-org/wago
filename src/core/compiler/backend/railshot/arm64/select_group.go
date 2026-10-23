//go:build arm64

package arm64

import (
	"os"
)

var selectGroupEnabled = os.Getenv("WAGO_ARM64_NO_SELECT_GROUP") != "1"

type selectGroupValue struct {
	kind  uint8 // 1 local, 2 constant, 3 mask, 4 expression, 5 selected value
	local int
	mask  uint32
	cost  int
}

type selectGroupPlan struct {
	end, maskLocal int
	mask           uint32
}

func (f *fn) emitSelectGroupGuard(plan selectGroupPlan, maskReg Reg, destination int) {
	if plan.mask == ^uint32(0) {
		// The union covers the whole word: no mask materialization is needed.
		if f.opt(optZeroBranch) {
			f.selectGroupSite = f.a.Cbz32(maskReg)
		} else {
			f.cmpImm(maskReg, 0, false)
			f.selectGroupSite = f.a.Bcond(condE)
		}
	} else {
		if !f.a.TstImm32(maskReg, plan.mask) {
			temporary, owned := f.intConstReadReg(storage{kind: stConst, typ: mtI32, cval: int64(plan.mask)}, maskOf(maskReg))
			f.a.TstReg(maskReg, temporary, true)
			if owned {
				f.release(temporary)
			}
		}
		f.selectGroupSite = f.a.Bcond(condE)
	}
	f.selectGroupEnd = plan.end
	f.selectGroupLocal = destination
	f.stats.peep("select-group-guard")
}

func (f *fn) finishSelectGroup(offset int) {
	if f.selectGroupEnd == 0 || offset != f.selectGroupEnd {
		return
	}
	f.patchBranch19(f.selectGroupSite, f.a.Len())
	f.setFactsForLocal(f.selectGroupLocal, 0)
	f.invalidateStoreForward()
	f.invalidateBoundsCert()
	f.selectGroupEnd = 0
}
