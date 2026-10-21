//go:build amd64 && (!tinygo || !wago_lean)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (f *fn) tryFuseAbstractArrayCastLen(nullable bool, reader *wasm.Reader) (bool, error) {
	if !gcSharedStubsEnabled || !f.gcArrayHelpers {
		return false, nil
	}
	look := *reader
	op, err := look.Byte()
	if err != nil || op != 0xfb {
		return false, nil
	}
	sub, err := look.U32()
	if err != nil {
		return false, err
	}
	if sub != 15 {
		return false, nil
	}
	*reader = look
	// Both branches have the same homes. Only the cold legacy/invalid-object
	// branch enters Go; successful casts and reads stay in the native activation.
	f.flush()
	f.spillLocalsForCall()
	f.storePinnedGlobals(false)
	slot := f.s.back().st.slotIndex()
	f.a.Load64(RAX, RSP, f.spillOff(slot))
	if regallocCheckEnabled {
		f.checkCallClobber()
	}
	site := f.a.CallRel32()
	f.relocs = append(f.relocs, f.newGCStubCallReloc(site, gcSharedStubAbstractArrayCastLen))
	f.stats.call("gcnative-leaf")
	f.a.TestSelf(RDX, false)
	fallback := f.a.JccPlaceholder(condE)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(fallback, f.a.Len())
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: -22})
	if nullable {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	} else {
		f.pushValue(storage{kind: stConst, typ: mtI32})
	}
	f.pushValue(storage{kind: stConst, typ: mtI32})
	anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	if err := f.callGCStructHelper(gcStructRefCast, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{anyref}); err != nil {
		return true, err
	}
	arrayref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapArray), false))
	if err := f.callGCStructHelper(gcArrayLen, []wasm.ValType{arrayref}, []wasm.ValType{wasm.I32}); err != nil {
		return true, err
	}
	result := f.materialize(f.popValue())
	if result != RAX {
		f.a.MovRegReg32(RAX, result)
	}
	f.release(result)
	f.pushReg(RAX, mtI32)
	f.a.PatchRel32(done, f.a.Len())
	f.stats.peep("gc-abstract-array-cast-len-fuse")
	return true, nil
}

// EAX is not narrowed until the full anyref word is checked. Return EAX=length,
// EDX=1 on success; EDX=0 requests the original two Go helpers. Null always takes
// that fallback, preserving both nullable and nonnullable cast/length traps.
// The optional encoding guard makes this code safe with legacy ABI-9 runtimes.
func emitModuleGCAbstractArrayCastLen(a *x64.Asm) {
	a.Push(R9)
	a.Push(R10)
	a.Push(R11)
	var failures []int
	fail := func(cc Cond) { failures = append(failures, a.JccPlaceholder(cc)) }
	a.MovReg64(R10, RAX)
	a.ShiftImm(5, R10, 32, true)
	a.TestSelf(R10, true)
	fail(condNE)
	a.TestSelf(RAX, false)
	fail(condE)
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	fail(condNE)
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.Load32(R10, R8, gc.NativeViewSubtypeEncodingOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeSubtypeEncodingKindBit), false)
	fail(condNE)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	fail(condAE)
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ShiftImm(4, RAX, 4, true)
	a.Add64(R9, RAX)
	a.LoadIdx(R10, R9, RSP, gc.NativeHandleSpaceOffset, 1, false, false)
	a.TestSelf(R10, false)
	fail(condE)
	a.AluRI(cmpDigit, R10, gc.NativeSpaceCount-1, false)
	fail(condA)
	a.ImulRI(R10, gc.NativeViewSpaceStride, true)
	a.LeaDisp(R11, R8, gc.NativeViewSpacesOffset)
	a.Add64(R11, R10)
	a.Load32(RDX, R11, gc.NativeSpaceBytesOffset)
	a.Load64(R11, R11, gc.NativeSpaceBaseOffset)
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, RDX)
	fail(condA)
	a.Sub32(RDX, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, RDX)
	fail(condA)
	a.AluRI(cmpDigit, RCX, int32(gc.HeaderSize), false)
	fail(condB)
	a.Add64(R11, RAX)
	a.AluRM(cmpRMcode, RCX, R11, 4, false)
	fail(condNE)
	a.Load32(RAX, R11, 0)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewSubtypeIntervalCountOffset, false)
	fail(condAE)
	a.Load64(R10, R8, gc.NativeViewSubtypeIntervalsOffset)
	a.TestSelf(R10, true)
	fail(condE)
	a.ImulRI(RAX, 8, true)
	a.LoadIdx(RAX, R10, RAX, 4, 4, false, false)
	a.AluRI(4, RAX, int32(gc.NativeSubtypeArrayKindBit), false)
	a.TestSelf(RAX, false)
	fail(condE)
	a.Load32(RAX, R11, 8)
	a.MovImm32(RDX, 1)
	done := a.JmpPlaceholder()
	failure := a.Len()
	a.MovImm32(RDX, 0)
	for _, site := range failures {
		a.PatchRel32(site, failure)
	}
	a.PatchRel32(done, a.Len())
	a.Pop(R11)
	a.Pop(R10)
	a.Pop(R9)
	a.Ret()
}
