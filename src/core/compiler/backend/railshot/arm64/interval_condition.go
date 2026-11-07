//go:build arm64

package arm64

import "os"

var regionalConditionFusionEnabled = os.Getenv("WAGO_ARM64_NO_REGIONAL_CONDITION_FUSION") != "1"

// Regional locals still need canonical homes at every edge. A pure condition
// can stay deferred while those homes are written: demoting its borrowed local
// references makes it read the same values from the now-current slots. The
// ordinary if/br_if lowering can then emit CMP+B.cond rather than spilling a
// materialized boolean and loading it again.
func (f *fn) clearIntervalCondition(op byte) bool {
	if !f.opt(optRegionalConditionFusion) || (op != 0x04 && op != 0x0d) ||
		!f.intervalControl || f.intervalActive == 0 || f.unreachable ||
		f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 {
		return false
	}
	top := f.s.back()
	if !isFusableCondition(top) {
		return false
	}
	count := 0
	for e := f.s.baseOfValentBlock(top); ; e = f.s.next(e) {
		count++
		if count > 64 {
			return false
		}
		if e.isDeferred() {
			if !isFusableCompare(e) {
				switch e.deferredOp() {
				case opAdd, opSub, opMul, opAnd, opOr, opXor, opShl, opShrS, opShrU:
				default:
					return false
				}
			}
		} else {
			if e.elemKind() != ekValue || (e.st.typ != mtI32 && e.st.typ != mtI64 && e.st.typ != mtF32 && e.st.typ != mtF64) {
				return false
			}
			switch e.st.kind {
			case stConst, stReg, stSlot, stLocalRef, stLocalReg:
			default:
				// In particular, a deferred memory read can borrow the very
				// regional register being relinquished. Leave its realization
				// and any trapping operation to the existing full flush.
				return false
			}
		}
		if e == top {
			break
		}
	}
	f.canonicalizeIntervalControlRegion(top)
	f.stats.peep("regional-condition-fusion")
	return true
}
