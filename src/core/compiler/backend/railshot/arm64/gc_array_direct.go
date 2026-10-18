//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// arrayScalarShortcut accesses pointer-free nursery arrays. Other spaces and
// all failed checks use the existing helper, including its precise traps.
// Operands are read from canonical slots before control-frame marshalling.
func (f *fn) arrayScalarShortcut(helper, typeIndex uint32, argSlot int) int {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray)
	if !ok || !layout.Type.Final || !layout.PointerFree || !layout.ElemLayout.DirectAccess {
		return -1
	}
	size := layout.ElemLayout.Size
	if size != 4 && size != 8 {
		return -1
	}
	var fallback []int
	cold := func(site int) { fallback = append(fallback, site) }
	f.ld64(X16, SP, f.spillOff(argSlot))
	cold(f.a.Cbz64(X16))
	f.a.LsrImm(X17, X16, 32, false)
	cold(f.a.Cbnz64(X17))
	f.a.TstImm64(X16, 1)
	cold(f.a.Bcond(condNE))
	f.a.LsrImm(X16, X16, 1, true)
	f.ld64(X9, linMemReg, -int32(abi.GCNativeViewPtrOffset))
	cold(f.a.Cbz64(X9))
	f.ld64(X9, X9, gc.NativeInstanceViewCollectorOffset)
	cold(f.a.Cbz64(X9))
	f.ld32(X17, X9, gc.NativeViewHandleCountOffset)
	f.cmpRR(X16, X17, false)
	cold(f.a.Bcond(condAE))
	f.ld64(X10, X9, gc.NativeViewHandlesOffset)
	f.a.AddShifted(X10, X10, X16, 4, false)
	f.ld32(X17, X10, gc.NativeHandleMetadataOffset)
	f.a.LsrImm(X17, X17, 16, true)
	f.a.AndImm32(X17, X17, 255)
	f.cmpImm(X17, uint32(gc.NativeSpaceNursery), false)
	cold(f.a.Bcond(condNE))
	f.ld32(X16, X10, gc.NativeHandleOffsetOffset)
	f.ld64(X10, X9, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBaseOffset))
	f.a.Add64(X10, X10, X16)
	// Resolve the module-local type into this collector's canonical domain.
	f.ld64(X9, linMemReg, -int32(abi.GCNativeViewPtrOffset))
	f.ld64(X9, X9, gc.NativeInstanceViewLocalTypesOffset)
	f.ld32(X17, X9, int32(typeIndex*4))
	f.ld32(X16, X10, 0)
	f.cmpRR(X16, X17, false)
	cold(f.a.Bcond(condNE))
	f.ld32(X16, SP, f.spillOff(argSlot+1)) // element index
	f.ld32(X17, X10, 8)                    // array length
	f.cmpRR(X16, X17, false)
	if helper == gcArrayGet {
		cold(f.a.Bcond(condAE))
	} else {
		cold(f.a.Bcond(condA))
		f.a.Sub32(X17, X17, X16)
		f.ld32(X9, SP, f.spillOff(argSlot+3)) // fill length
		f.cmpRR(X9, X17, false)
		cold(f.a.Bcond(condA))
	}
	shift := uint8(2)
	if size == 8 {
		shift = 3
	}
	f.a.AddShifted(X10, X10, X16, shift, false)
	if helper == gcArrayGet {
		if size == 4 {
			f.ld32(X16, X10, int32(gc.PayloadOffset))
		} else {
			f.ld64(X16, X10, int32(gc.PayloadOffset))
		}
		f.ld64(X11, linMemReg, -int32(offCustomCtx))
		f.st64(X11, hcResults, X16)
	} else {
		if size == 4 {
			f.ld32(X16, SP, f.spillOff(argSlot+2))
		} else {
			f.ld64(X16, SP, f.spillOff(argSlot+2))
		}
		f.a.AddImm64(X10, X10, gc.PayloadOffset)
		f.emitGCScalarFill(X10, X9, X16, size)
	}
	done := f.a.Branch()
	for _, site := range fallback {
		f.patchBranch19(site, f.a.Len())
	}
	f.stats.peep("gc-array-scalar-native")
	return done
}
