//go:build arm64

package arm64

import "math/bits"

// A pure local numeric leaf has no ABI, context or resource effect to reproduce.
// Inline a bounded affine table's result while retaining the original index
// check. The module-owned entry proof is usable only with the private immutable
// table proof; exported or mutable tables always retain ordinary dispatch.
func (f *fn) tryAffineI32TableCall(table uint32, key uint64) bool {
	p := f.affineTable
	if table != 0 || p.Count == 0 || p.TypeKey != key || !f.immutableLocalTable || !f.opt(optInline) || f.unreachable || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || f.localBase != 0 || len(f.customInstructions) != 0 {
		return false
	}
	// Evaluate older deferred traps before the index, then materialize the argument
	// before the call's bounds check, including when the leaf discards its value.
	f.materializePendingTraps()
	index := f.materialize(f.popValue())
	f.pinned = f.pinned.add(index)
	value := f.materialize(f.popValue())
	f.pinned = f.pinned.add(value)
	f.cmpImm(index, p.Count, false)
	f.trapIf(condAE, trapIndirectOOB)
	multiply := func(reg Reg, k uint32) {
		switch {
		case k == 0:
			f.a.MovImm32(reg, 0)
		case k == 1:
		case k&(k-1) == 0:
			f.a.LslImm(reg, reg, uint8(bits.TrailingZeros32(k)), true)
		default:
			tmp := f.allocReg(0)
			f.a.MovImm32(tmp, int32(k))
			f.a.Mul32(reg, reg, tmp)
			f.release(tmp)
		}
	}
	multiply(value, p.Scale)
	if p.Step != 0 {
		if p.Step&(p.Step-1) == 0 {
			f.a.AddShifted(value, value, index, uint8(bits.TrailingZeros32(p.Step)), true)
		} else {
			multiply(index, p.Step)
			f.a.Add32(value, value, index)
		}
	}
	if p.Bias != 0 {
		if p.Bias <= 4095 {
			f.a.AddImm32(value, value, p.Bias)
		} else {
			f.a.MovImm32(index, int32(p.Bias))
			f.a.Add32(value, value, index)
		}
	}
	f.pinned = f.pinned.remove(index).remove(value)
	f.release(index)
	f.pushReg(value, mtI32)
	f.stats.peep("affine-i32-table-call-inline")
	return true
}
