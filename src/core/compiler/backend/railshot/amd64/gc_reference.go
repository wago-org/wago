//go:build amd64

package amd64

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
	f.a.Load64(ref, RSP, f.spillOff(slot))
	f.a.MovReg64(tag, ref)
	f.a.ShiftImm(5, tag, 32, true)
	f.a.TestSelf(tag, true)
	foreign := f.a.JccPlaceholder(condNE)
	f.a.MovRegReg32(tag, ref)
	f.a.AluRI(4, tag, 1, false)
	f.a.TestSelf(tag, false)
	i31 := f.a.JccPlaceholder(condNE)
	f.a.TestSelf(ref, true)
	object := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(i31, f.a.Len())
	if sub < 22 {
		if heap == -15 {
			f.a.MovImm32(tag, 0)
		}
		if sub == 21 {
			f.a.TestSelf(ref, true)
			f.a.SetccReg(condE, ref)
			f.a.AluRR(aluTable[opOr].rr, tag, ref, false)
		}
		f.a.Store64(RSP, f.spillOff(slot), tag)
	} else {
		if heap == -15 {
			f.a.TestSelf(ref, true)
			if sub == 23 {
				f.trapIf(condNE, trapCastFailure)
			} else {
				f.trapAlways(trapCastFailure)
			}
		} else if sub == 22 {
			f.a.TestSelf(ref, true)
			f.trapIf(condE, trapCastFailure)
		}
		// A successful cast retains the original null/i31 slot unchanged.
	}
	done := f.a.JmpPlaceholder()
	f.release(tag)
	f.release(ref)
	f.a.PatchRel32(foreign, f.a.Len())
	f.a.PatchRel32(object, f.a.Len())
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: heap})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(sub & 1)})
	f.pushValue(storage{kind: stConst, typ: mtI32})
	anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	helper, result := uint32(gcStructRefTest), wasm.I32
	if sub >= 22 {
		helper, result = gcStructRefCast, anyref
	}
	if err := f.callGCStructHelper(helper, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
		return err
	}
	f.flush()
	f.a.PatchRel32(done, f.a.Len())
	f.stats.peep("gc-null-i31-ref")
	return nil
}
