//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// emitNullI31Reference handles only null and compact i31 words. Objects retain
// the helper's handle validation; high-bit words retain foreign-owner checks.
// Only successful casts bypass Go, so failures need no extra native trap stub.
func (f *fn) emitNullI31Reference(sub uint32, heap int64) error {
	// Both edges need the same stack and pin homes before a conditional call.
	f.flush()
	f.spillLocalsForCall()
	f.storePinnedGlobals(false)
	slot := f.s.back().st.slotIndex()
	ref := f.allocReg(0)
	tag := f.allocReg(maskOf(ref))
	f.ld64(ref, SP, f.spillOff(slot))
	foreign := -1
	var object int
	if heap == -15 && sub == 23 { // only null can pass a nullable none cast
		object = f.zeroBranch(ref, true, false)
	} else {
		f.a.LsrImm(tag, ref, 32, false)
		foreign = f.zeroBranch(tag, true, false)
		f.a.AndImm32(tag, ref, 1)
		if sub == 22 { // only compact i31 can pass the non-nullable cast
			object = f.zeroBranch(tag, false, true)
		} else {
			i31 := f.zeroBranch(tag, false, false)
			object = f.zeroBranch(ref, true, false)
			f.patchBranch19(i31, f.a.Len())
		}
	}
	if sub < 22 {
		if heap == -15 {
			f.a.MovImm32(tag, 0)
		}
		if sub == 21 {
			f.cmpImm(ref, 0, true)
			f.a.Cset32(ref, condE)
			f.a.Orr32(tag, tag, ref)
		}
		f.a.MovReg32(X0, tag)
	} else if ref != X0 {
		f.a.MovReg64(X0, ref)
	}
	done := f.a.Branch()
	f.release(tag)
	f.release(ref)
	if foreign >= 0 {
		f.patchBranch19(foreign, f.a.Len())
	}
	f.patchBranch19(object, f.a.Len())
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: heap})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(sub & 1)})
	f.pushValue(storage{kind: stConst, typ: mtI32})
	anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	helper, result := gcStructRefTest, wasm.I32
	if sub >= 22 {
		helper, result = gcStructRefCast, anyref
	}
	if err := f.callGCStructHelper(helper, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
		return err
	}
	output := f.popValue()
	gcRoot := output.st.hasGCRoot()
	value := f.materialize(output)
	if value != X0 {
		if sub < 22 {
			f.a.MovReg32(X0, value)
		} else {
			f.a.MovReg64(X0, value)
		}
	}
	f.release(value)
	f.pushReg(X0, mtOf(result)).st.setGCRoot(gcRoot)
	f.patchBranch26(done, f.a.Len())
	f.stats.peep("gc-null-i31-ref")
	return nil
}
