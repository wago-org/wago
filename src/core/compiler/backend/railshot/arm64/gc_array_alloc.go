//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// arrayAllocationShortcut uses a collector-reserved identity for small scalar
// arrays. The ordinary helper owns all safepoints and replenishes reservations.
// All operands and locals are homed before entering; X2-X7 are caller scratch.
func (f *fn) arrayAllocationShortcut(helper, typeIndex uint32, argSlot int) int {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray)
	if !ok || !layout.Type.Final || !layout.PointerFree || !layout.ElemLayout.DirectAccess {
		return -1
	}
	elemSize := layout.ElemLayout.Size
	if elemSize != 4 && elemSize != 8 {
		return -1
	}
	var fallback []int
	cold := func(site int) { fallback = append(fallback, site) }
	lengthOff := f.spillOff(argSlot)
	if helper == gcArrayAllocUniform {
		lengthOff = f.spillOff(argSlot + 1)
	}
	f.ld32(X2, SP, lengthOff)
	// Bound generated fill work and avoid all size arithmetic overflow.
	f.cmpImm(X2, 64, false)
	cold(f.a.Bcond(condA))
	shift := uint8(2)
	if elemSize == 8 {
		shift = 3
	}
	f.a.LslImm(X3, X2, shift, true)
	f.a.AddImm32(X3, X3, gc.PayloadOffset+7)
	f.a.AndImm32(X3, X3, ^uint32(7))
	f.ld64(X9, linMemReg, -int32(abi.GCNativeViewPtrOffset))
	cold(f.a.Cbz64(X9))
	f.ld64(X9, X9, gc.NativeInstanceViewCollectorOffset)
	cold(f.a.Cbz64(X9))
	f.ld32(X16, X9, gc.NativeViewNurseryObjectMaxBytesOffset)
	f.cmpRR(X3, X16, false)
	cold(f.a.Bcond(condAE))
	f.ld64(X10, X9, gc.NativeViewStructAllocStateOffset)
	cold(f.a.Cbz64(X10))
	f.ld32(X4, X10, gc.NativeStructAllocCursorOffset)
	f.ld32(X16, X10, gc.NativeStructAllocCountOffset)
	f.cmpImm(X16, gc.NativeStructAllocHandleCapacity, false)
	cold(f.a.Bcond(condA))
	f.cmpRR(X4, X16, false)
	cold(f.a.Bcond(condAE))
	f.a.AddShifted(X17, X10, X4, 2, false)
	f.ld32(X5, X17, gc.NativeStructAllocHandlesOffset)
	f.ld64(X6, X9, gc.NativeViewAllocationCountOffset)
	cold(f.a.Cbz64(X6))
	f.ld32(X7, X10, gc.NativeStructAllocChunkCursorOffset)
	f.a.AddImm32(X7, X7, 7)
	f.a.AndImm32(X7, X7, ^uint32(7))
	f.ld32(X16, X10, gc.NativeStructAllocChunkEndOffset)
	f.cmpRR(X7, X16, false)
	cold(f.a.Bcond(condA))
	f.a.Sub32(X16, X16, X7)
	f.cmpRR(X3, X16, false)
	cold(f.a.Bcond(condA))
	f.ld64(X16, X9, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBaseOffset))
	cold(f.a.Cbz64(X16))
	f.a.Add64(X16, X16, X7) // destination header
	// No fallbacks or safepoints after this point. Publish only initialized data.
	f.ld64(X17, linMemReg, -int32(abi.GCNativeViewPtrOffset))
	f.ld64(X17, X17, gc.NativeInstanceViewLocalTypesOffset)
	f.ld32(X17, X17, int32(typeIndex*4))
	f.st32(X16, 0, X17)
	f.st32(X16, 4, X3)
	f.st32(X16, 8, X2)
	f.a.MovImm32(X17, int32(gc.FlagPointerFree))
	f.st32(X16, 12, X17)
	f.a.AddImm64(X16, X16, gc.PayloadOffset)
	if helper == gcArrayAllocUniform {
		if elemSize == 4 {
			f.ld32(X17, SP, f.spillOff(argSlot))
		} else {
			f.ld64(X17, SP, f.spillOff(argSlot))
		}
	} else {
		f.a.MovImm32(X17, 0)
	}
	f.emitGCScalarFill(X16, X2, X17, elemSize)
	f.ld64(X16, X9, gc.NativeViewHandlesOffset)
	f.a.AddShifted(X16, X16, X5, 4, false)
	f.st32(X16, gc.NativeHandleOffsetOffset, X7)
	f.st32(X16, gc.NativeHandleSizeOffset, X3)
	f.st32(X16, gc.NativeHandleCardSlotOffset, ZR)
	f.a.Add32(X7, X7, X3)
	f.st32(X10, gc.NativeStructAllocChunkCursorOffset, X7)
	f.ld64(X17, X6, 0)
	f.a.AddImm64(X17, X17, 1)
	f.st64(X6, 0, X17)
	f.a.MovImm32(X17, int32(gc.NativeSpaceNursery)<<16)
	f.st32(X16, gc.NativeHandleMetadataOffset, X17)
	f.a.AddImm32(X4, X4, 1)
	f.st32(X10, gc.NativeStructAllocCursorOffset, X4)
	f.a.LslImm(X5, X5, 1, true)
	f.ld64(X11, linMemReg, -int32(offCustomCtx))
	f.st64(X11, hcResults, X5)
	done := f.a.Branch()
	for _, site := range fallback {
		f.patchBranch19(site, f.a.Len())
	}
	f.stats.peep("gc-array-allocation-native")
	return done
}
