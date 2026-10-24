//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Sink scalar arithmetic into its global's V register. Earlier reads below the
// operands materialize before overwrite, preserving global.get read timing.
func (f *fn) tryFbinGlobalSet(r *wasm.Reader, vop func(dst, s1, s2 Reg, f64 bool), f64 bool) (bool, error) {
	save := r.Offset()
	_, err := r.Byte()
	if err != nil {
		return false, err
	}
	x, err := r.U32()
	if err != nil {
		return false, err
	}
	reg, ok := f.pinnedGlobalValueReg(x)
	gt, valid := f.globalType(x)
	if !ok || !valid || !globalRegIsFloat(f.globalReg[x]) || wasm.EqualValType(wasm.GlobalValueType(gt), wasm.F64) != f64 {
		return false, r.JumpTo(save)
	}
	right := f.s.back()
	if right == nil {
		return false, r.JumpTo(save)
	}
	left := f.s.prev(f.s.baseOfValentBlock(right))
	f.invalidateBoundsCertFor(2, x)
	f.materializePendingTraps()
	f.realizeGlobalRefs(x, left)
	f.fbinInto(reg, vop, 0, f64)
	f.globalReg[x] |= globalRegDirty
	f.stats.peep("float-global-sink")
	return true, nil
}
