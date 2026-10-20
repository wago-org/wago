//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

// emitNullI31Reference handles only null and compact i31 words. Objects retain
// the helper's handle validation; high-bit words retain foreign-owner checks.
// Only successful casts bypass Go, so failures need no extra native trap stub.
func (f *fn) emitNullI31Reference(sub uint32, heap int64) error {
	// Both edges need the same stack and pin homes before a conditional call.
	f.flush()
	f.spillLocalsForCall()
	f.storePinnedGlobals(false)
	slot := f.s.back().st.slotIndex()
	// The leaf preserves the original full-width word and cannot collect. The
	// original stack slot remains authoritative for the conditional Go fallback.
	f.a.Load64(RAX, RSP, f.spillOff(slot))
	if regallocCheckEnabled {
		f.checkCallClobber()
	}
	site := f.a.CallRel32()
	f.relocs = append(f.relocs, f.newGCStubCallReloc(site, gcSharedStubNullI31Classifier))
	f.stats.call("gcnative-leaf")
	var fallback int
	if sub == 22 { // only compact i31 passes the non-nullable cast
		f.a.AluRI(cmpDigit, RDX, 1, false)
		fallback = f.a.JccPlaceholder(condNE)
	} else if heap == -15 && sub == 23 { // only null passes the nullable none cast
		f.a.TestSelf(RDX, false)
		fallback = f.a.JccPlaceholder(condNE)
	} else {
		f.a.AluRI(cmpDigit, RDX, 2, false)
		fallback = f.a.JccPlaceholder(condE)
	}
	if sub < 22 {
		if heap == -15 {
			if sub == 21 {
				f.a.AluRI(6, RDX, 1, false)
			} else {
				f.a.XorSelf32(RDX)
			}
		} else if sub == 21 {
			f.a.MovImm32(RDX, 1)
		}
		f.a.MovRegReg32(RAX, RDX)
	}
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(fallback, f.a.Len())
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

// emitModuleNullI31Classifier is a noncollecting leaf. RAX is the unchanged
// full-width reference; RDX returns null=0, compact i31=1, or needs-Go=2.
// No other registers are changed. Stack values remain owned by the caller.
func emitModuleNullI31Classifier(a *x64.Asm) {
	a.MovRegReg32(RDX, RAX)
	a.Cmp64(RDX, RAX)
	foreign := a.JccPlaceholder(condNE)
	a.AluRI(4, RDX, 1, false)
	i31 := a.JccPlaceholder(condNE)
	a.TestSelf(RAX, true)
	null := a.JccPlaceholder(condE)
	a.PatchRel32(foreign, a.Len())
	a.MovImm32(RDX, 2)
	a.PatchRel32(i31, a.Len())
	a.PatchRel32(null, a.Len())
	a.Ret()
}
