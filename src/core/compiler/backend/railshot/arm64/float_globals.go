//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Spend only unused local-pin V registers, capped at four hot defined globals.
// Imported globals may alias; calls, EH and helper-bearing GC/custom lowering
// remain outside this proof. Mutable cells are synchronized on return/trap.
func (f *fn) assignPinnedFloatGlobals(hints []shared.GlobalHint, hasCall bool, loopMin uint32, used, limit int) {
	if hasCall || f.makesCalls || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || used >= limit {
		return
	}
	var candidates [4]gpCand
	globals := candidates[:0]
	capacity := min(len(candidates), limit-used)
	for _, hint := range hints {
		if hint.Score < loopMin || int(hint.Index) < f.m.ImportedGlobalCount() || f.isModuleGlobal(int(hint.Index)) {
			continue
		}
		gt, ok := f.globalType(hint.Index)
		if !ok || !gt.Mutable {
			continue
		}
		typ := wasm.GlobalValueType(gt)
		if !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) {
			continue
		}
		globals = insertGPCandidate(globals, gpCand{global: true, idx: hint.Index, score: hint.Score}, capacity)
	}
	for i, global := range globals {
		reg := pinnedFLocalRegs[used+i]
		f.globalReg[global.idx] = reg | globalRegFloat
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(reg)
		f.stats.addPinnedGlobalValue()
		f.stats.peep("float-global-value-pin")
	}
}

func (f *fn) storePinnedGlobalValue(g int, state Reg, cell Reg) {
	reg := globalRegValue(state)
	if globalRegIsFloat(state) {
		gt, _ := f.globalType(uint32(g))
		if wasm.EqualValType(wasm.GlobalValueType(gt), wasm.F64) {
			f.a.StrD(cell, 0, reg)
		} else {
			f.a.StrS(cell, 0, reg)
		}
	} else if f.globalIs64(g) {
		f.st64(cell, 0, reg)
	} else {
		f.st32(cell, 0, reg)
	}
}

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
