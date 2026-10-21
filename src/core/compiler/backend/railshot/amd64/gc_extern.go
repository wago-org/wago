//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Only full-width null can skip the conversion identity and ownership bridge.
func (f *fn) emitNullExternConversion(sub uint32) error {
	helper, source, result := uint32(gcAnyConvertExtern), wasm.ExternRef, wasm.AnyRef
	if sub == 27 {
		helper, source, result = gcExternConvertAny, wasm.AnyRef, wasm.ExternRef
	}
	if top := f.s.back(); top != nil && top.isValue() && top.st.kind == stConst && top.st.cval == 0 {
		f.materializePendingTraps()
		f.dropValue()
		value := f.pushValue(storage{kind: stConst, typ: mtI64})
		f.setStackGCRoot(value, f.gcFrameRoots != nil && f.gcFrameRoots.Candidate && gcFrameRefType(f.m, result))
		f.stats.peep("gc-null-extern-convert")
		return nil
	}
	// Both continuations use canonical homes before a conditional parked call.
	f.flush()
	f.spillLocalsForCall()
	f.storePinnedGlobals(false)
	slot := f.s.back().st.slotIndex()
	f.a.Load64(RAX, RSP, f.spillOff(slot))
	f.a.TestSelf(RAX, true)
	slow := f.a.JccPlaceholder(condNE)
	done := f.a.JmpPlaceholder() // RAX already holds the null result
	f.a.PatchRel32(slow, f.a.Len())
	if err := f.callGCStructHelper(helper, []wasm.ValType{source}, []wasm.ValType{result}); err != nil {
		return err
	}
	output := f.popValue()
	gcRoot := output.st.hasGCRoot()
	value := f.materialize(output)
	if value != RAX {
		f.a.MovReg64(RAX, value)
	}
	f.release(value)
	f.setStackGCRoot(f.pushReg(RAX, mtI64), gcRoot)
	f.a.PatchRel32(done, f.a.Len())
	f.stats.peep("gc-null-extern-convert")
	return nil
}
