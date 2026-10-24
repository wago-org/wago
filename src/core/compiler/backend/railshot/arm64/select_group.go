//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/bits"
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

// scanSelectGroup proves one unchanged bitmask controls a bounded pure update
// chain. A private reader and fixed-size abstract stack keep compile work bounded.
func (f *fn) scanSelectGroup(reader *wasm.Reader, destination int) (selectGroupPlan, bool) {
	r := *reader
	var values [16]selectGroupValue
	values[0] = selectGroupValue{kind: 1, local: destination}
	depth, updates, totalCost := 1, 0, 0
	maskLocal := -1
	union := uint32(0)
	pending := false
	for instructions := 0; instructions < 200 && r.HasNext(); instructions++ {
		op, err := r.Byte()
		if err != nil {
			break
		}
		switch op {
		case 0x20:
			x, err := r.U32()
			if err != nil || int(x) >= f.nLocals || f.localType[x] != mtI32 || depth == len(values) {
				return selectGroupPlan{}, false
			}
			values[depth] = selectGroupValue{kind: 1, local: int(x)}
			depth++
		case 0x41:
			v, err := r.I32()
			if err != nil || depth == len(values) {
				return selectGroupPlan{}, false
			}
			values[depth] = selectGroupValue{kind: 2, mask: uint32(v)}
			depth++
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78:
			if depth < 2 || pending {
				return selectGroupPlan{}, false
			}
			a, b := values[depth-2], values[depth-1]
			cost := 1
			if op == 0x6c {
				cost = 4
			}
			value := selectGroupValue{kind: 4, cost: a.cost + b.cost + cost}
			if op == 0x71 {
				if a.kind == 2 {
					a, b = b, a
				}
				if a.kind == 1 && b.kind == 2 && b.mask != 0 {
					value.kind = 3
					value.local = a.local
					value.mask = b.mask
				}
			}
			values[depth-2] = value
			depth--
		case 0x1b:
			if depth != 3 || pending {
				return selectGroupPlan{}, false
			}
			yes, no, condition := values[0], values[1], values[2]
			if no.kind != 1 || no.local != destination || condition.kind != 3 || condition.local == destination {
				return selectGroupPlan{}, false
			}
			if maskLocal >= 0 && maskLocal != condition.local {
				return selectGroupPlan{}, false
			}
			maskLocal = condition.local
			union |= condition.mask
			totalCost += yes.cost
			updates++
			depth = 1
			values[0] = selectGroupValue{kind: 5}
			pending = true
		case 0x21, 0x22:
			x, err := r.U32()
			if err != nil || int(x) != destination || !pending || depth != 1 {
				return selectGroupPlan{}, false
			}
			pending = false
			if op == 0x21 {
				if updates >= 3 && totalCost >= 24 && bits.OnesCount32(union) >= 6 {
					return selectGroupPlan{end: r.Offset(), maskLocal: maskLocal, mask: union}, true
				}
				return selectGroupPlan{}, false
			}
			values[0] = selectGroupValue{kind: 1, local: destination}
		default:
			return selectGroupPlan{}, false
		}
	}
	return selectGroupPlan{}, false
}

func (f *fn) beginSelectGroup(reader *wasm.Reader) {
	if !f.opt(optSelectGroupGuard) || f.selectGroupEnd != 0 || f.unreachable || f.usesCalls || f.localBase != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 {
		return
	}
	top := f.s.back()
	if top == nil || top.elemKind() != ekValue || top.st.kind != stLocalReg || top.st.typ != mtI32 {
		return
	}
	destination := top.st.index()
	if destination < f.nParams || !f.canonicalI32Local(destination) {
		return
	}
	if _, floating, pinned := f.pinReg(destination); !pinned || floating {
		return
	}
	plan, okay := f.scanSelectGroup(reader, destination)
	if !okay {
		return
	}
	maskReg, floating, pinned := f.pinReg(plan.maskLocal)
	if !pinned || floating {
		return
	}
	// A pinned declared local may still represent a lazy zero. The guard
	// reads it before the first local.get in the group can recover it.
	f.recoverLocal(plan.maskLocal)
	f.flushBelow(top)
	f.invalidateGlobalsCache()
	f.invalidateStoreForward()
	f.invalidateBoundsCert()
	f.emitSelectGroupGuard(plan, maskReg, destination)
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
