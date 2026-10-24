//go:build amd64

package amd64

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
	f.a.Load64(ref, RSP, f.spillOff(slot))
	foreign := -1
	var object int
	if heap == -15 && sub == 23 { // only null can pass a nullable none cast
		f.a.TestSelf(ref, true)
		object = f.a.JccPlaceholder(condNE)
	} else {
		f.a.MovRegReg32(tag, ref)
		f.a.Cmp64(tag, ref)
		foreign = f.a.JccPlaceholder(condNE)
		f.a.AluRI(4, tag, 1, false)
		if sub == 22 { // only compact i31 can pass the non-nullable cast
			object = f.a.JccPlaceholder(condE)
		} else {
			i31 := f.a.JccPlaceholder(condNE)
			f.a.TestSelf(ref, true)
			object = f.a.JccPlaceholder(condNE)
			f.a.PatchRel32(i31, f.a.Len())
		}
	}
	if sub < 22 {
		if heap == -15 {
			f.a.MovImm32(tag, 0)
		}
		if sub == 21 {
			f.a.TestSelf(ref, true)
			f.a.SetccReg(condE, ref)
			f.a.AluRR(aluTable[opOr].rr, tag, ref, false)
		}
		f.a.MovRegReg32(RAX, tag)
	} else if ref != RAX {
		f.a.MovReg64(RAX, ref)
	}
	done := f.a.JmpPlaceholder()
	f.release(tag)
	f.release(ref)
	if foreign >= 0 {
		f.a.PatchRel32(foreign, f.a.Len())
	}
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
	output := f.popValue()
	gcRoot := output.st.hasGCRoot()
	value := f.materialize(output)
	if value != RAX {
		if sub < 22 {
			f.a.MovRegReg32(RAX, value)
		} else {
			f.a.MovReg64(RAX, value)
		}
	}
	f.release(value)
	f.setStackGCRoot(f.pushReg(RAX, mtOf(result)), gcRoot)
	f.a.PatchRel32(done, f.a.Len())
	f.stats.peep("gc-null-i31-ref")
	return nil
}
