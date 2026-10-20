//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// emitNullI31Reference handles only null and compact i31 words. Objects retain
// the helper's handle validation; high-bit words retain foreign-owner checks.
func (f *fn) emitNullI31Reference(sub uint32, heap int64) error {
	f.flush()
	f.spillLocalsForCall()
	f.storePinnedGlobals(false)
	slot := f.s.back().st.slotIndex()
	ref := f.allocReg(0)
	tag := f.allocReg(maskOf(ref))
	f.ld64(ref, SP, f.spillOff(slot))
	f.a.LsrImm(tag, ref, 32, false)
	foreign := f.zeroBranch(tag, true, false)
	f.a.AndImm32(tag, ref, 1)
	i31 := f.zeroBranch(tag, false, false)
	object := f.zeroBranch(ref, true, false)
	f.patchBranch19(i31, f.a.Len())
	if sub < 22 {
		if heap == -15 {
			f.a.MovImm32(tag, 0)
		}
		if sub == 21 {
			f.cmpImm(ref, 0, true)
			f.a.Cset32(ref, condE)
			f.a.Orr32(tag, tag, ref)
		}
		f.st64(SP, f.spillOff(slot), tag)
	} else {
		if heap == -15 {
			if sub == 23 {
				f.trapIfZero(ref, true, false, trapCastFailure)
			} else {
				f.trapAlways(trapCastFailure)
			}
		} else if sub == 22 {
			f.trapIfZero(ref, true, true, trapCastFailure)
		}
		// A successful cast retains the original null/i31 slot unchanged.
	}
	done := f.a.Branch()
	f.release(tag)
	f.release(ref)
	f.patchBranch19(foreign, f.a.Len())
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
	f.flush()
	f.patchBranch26(done, f.a.Len())
	f.stats.peep("gc-null-i31-ref")
	return nil
}
