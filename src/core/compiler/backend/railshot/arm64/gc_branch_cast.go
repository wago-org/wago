//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// emitNativeBranchCastTest uses the same compact handle checks and immutable
// canonical subtype intervals as AMD64's defined-type test. The original
// reference remains in its rooted stack slot; only the copied operand changes.
func (f *fn) emitNativeBranchCastTest(typeIndex uint32, nullable bool) {
	object := f.popValue()
	f.flush()
	ref := f.materialize(object)
	avoid := maskOf(ref, X16, X17)
	view := f.allocReg(avoid)
	avoid = avoid.union(maskOf(view))
	handle := f.allocReg(avoid)
	avoid = avoid.union(maskOf(handle))
	space := f.allocReg(avoid)
	avoid = avoid.union(maskOf(space))
	target := f.allocReg(avoid)
	avoid = avoid.union(maskOf(target))
	tmp := f.allocReg(avoid)
	var misses [4]int
	n := 0
	miss := func(cond Cond) { misses[n] = f.a.Bcond(cond); n++ }

	// Foreign anyrefs cannot satisfy a defined collector type.
	f.a.LsrImm(tmp, ref, 32, false)
	f.cmpImm(tmp, 0, true)
	miss(condNE)
	nonNull := f.a.Cbnz32(ref)
	if nullable {
		f.a.MovImm32(ref, 1)
	}
	nullDone := f.a.Branch()
	f.patchBranch19(nonNull, f.a.Len())
	f.a.TstImm64(ref, 1)
	miss(condNE) // i31 is not a struct or array

	f.ld64(view, linMemReg, -int32(abi.GCNativeViewPtrOffset))
	f.ld64(target, view, gc.NativeInstanceViewLocalTypesOffset)
	if typeIndex <= 4095 {
		f.ld32(target, target, int32(typeIndex*4))
	} else {
		f.a.MovImm64(tmp, uint64(typeIndex)*4)
		f.a.Add64(target, target, tmp)
		f.ld32(target, target, 0)
	}
	f.ld64(view, view, gc.NativeInstanceViewCollectorOffset)
	f.a.LsrImm(ref, ref, 1, true)
	f.ld32(tmp, view, gc.NativeViewHandleCountOffset)
	f.cmpRR(ref, tmp, false)
	f.trapIf(condAE, trapCastFailure)
	f.ld64(handle, view, gc.NativeViewHandlesOffset)
	f.a.AddShifted(handle, handle, ref, 4, false)
	f.ld32(space, handle, gc.NativeHandleMetadataOffset)
	f.a.LsrImm(space, space, 16, true)
	f.a.AndImm32(space, space, 255)
	f.trapIfZero(space, false, true, trapCastFailure)
	f.cmpImm(space, gc.NativeSpaceCount-1, false)
	f.trapIf(condA, trapCastFailure)
	f.a.AddShifted(space, view, space, 4, false)
	f.a.AddImm64(space, space, gc.NativeViewSpacesOffset)
	f.ld32(tmp, space, gc.NativeSpaceBytesOffset)
	f.ld64(space, space, gc.NativeSpaceBaseOffset)
	f.ld32(ref, handle, gc.NativeHandleOffsetOffset)
	f.cmpRR(ref, tmp, false)
	f.trapIf(condA, trapCastFailure)
	f.a.Sub32(tmp, tmp, ref)
	f.ld32(handle, handle, gc.NativeHandleSizeOffset)
	f.cmpRR(handle, tmp, false)
	f.trapIf(condA, trapCastFailure)
	f.cmpImm(handle, gc.HeaderSize, false)
	f.trapIf(condB, trapCastFailure)
	f.a.Add64(space, space, ref)
	f.ld32(ref, space, 0) // actual canonical type ID

	// required.pre <= actual.pre && actual.post <= required.post.
	f.ld32(tmp, view, gc.NativeViewSubtypeIntervalCountOffset)
	f.cmpRR(ref, tmp, false)
	f.trapIf(condAE, trapCastFailure)
	f.cmpRR(target, tmp, false)
	f.trapIf(condAE, trapCastFailure)
	f.ld64(view, view, gc.NativeViewSubtypeIntervalsOffset)
	f.trapIfZero(view, true, true, trapCastFailure)
	f.a.AddShifted(handle, view, ref, 3, false)
	f.ld64(handle, handle, 0)
	f.a.AddShifted(target, view, target, 3, false)
	f.ld64(target, target, 0)
	f.a.LsrImm(ref, handle, 32, false)
	f.a.LsrImm(tmp, target, 32, false)
	f.cmpRR(ref, tmp, false)
	miss(condB)
	f.cmpRR(handle, target, false)
	miss(condA)
	f.a.MovImm32(ref, 1)
	done := f.a.Branch()
	for _, site := range misses[:n] {
		f.patchBranch19(site, f.a.Len())
	}
	f.a.MovImm32(ref, 0)
	f.patchBranch26(nullDone, f.a.Len())
	f.patchBranch26(done, f.a.Len())
	for _, reg := range [...]Reg{view, handle, space, target, tmp} {
		f.release(reg)
	}
	f.pushReg(ref, mtI32)
}
