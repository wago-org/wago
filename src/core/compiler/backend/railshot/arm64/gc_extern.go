//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Only full-width null can skip the conversion identity and ownership bridge.
func (f *fn) emitNullExternConversion(sub uint32) error {
	helper, source, result := gcAnyConvertExtern, wasm.ExternRef, wasm.AnyRef
	if sub == 27 {
		helper, source, result = gcExternConvertAny, wasm.AnyRef, wasm.ExternRef
	}
	if top := f.s.back(); top != nil && top.elemKind() == ekValue && top.st.kind == stConst && top.st.cval == 0 {
		f.materializePendingTraps()
		f.dropValue()
		value := f.pushValue(storage{kind: stConst, typ: mtI64})
		value.st.setGCRoot(f.tracksGCFrameRoots() && arm64GCFrameRefType(f.m, result))
		f.stats.peep("gc-null-extern-convert")
		return nil
	}
	// Both continuations use canonical homes before a conditional parked call.
	f.flush()
	f.spillLocalsForCall()
	f.storePinnedGlobals(false)
	slot := f.s.back().st.slotIndex()
	f.ld64(X0, SP, f.spillOff(slot))
	slow := f.zeroBranch(X0, true, false)
	done := f.a.Branch() // X0 already holds the null result
	f.patchBranch19(slow, f.a.Len())
	if err := f.callGCStructHelper(helper, []wasm.ValType{source}, []wasm.ValType{result}); err != nil {
		return err
	}
	output := f.popValue()
	gcRoot := output.st.hasGCRoot()
	value := f.materialize(output)
	if value != X0 {
		f.a.MovReg64(X0, value)
	}
	f.release(value)
	f.pushReg(X0, mtI64).st.setGCRoot(gcRoot)
	f.patchBranch26(done, f.a.Len())
	f.stats.peep("gc-null-extern-convert")
	return nil
}
