//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// arrayLenShortcut reads a validator-proven arrayref from its canonical slot.
// No call or collection occurs between checked resolution and the header load.
// Null, stale and malformed handles keep the original helper and its traps.
func (f *fn) arrayLenShortcut(argSlot int) int {
	var fallback [11]int
	n := 0
	cold := func(site int) { fallback[n] = site; n++ }
	f.ld64(X16, SP, f.spillOff(argSlot))
	cold(f.a.Cbz64(X16))
	f.a.LsrImm(X17, X16, 32, false)
	cold(f.a.Cbnz64(X17))
	f.a.TstImm64(X16, 1)
	cold(f.a.Bcond(condNE))
	f.a.LsrImm(X16, X16, 1, true)
	f.ld64(X9, linMemReg, -int32(abi.GCNativeViewPtrOffset))
	f.ld64(X9, X9, gc.NativeInstanceViewCollectorOffset)
	f.ld32(X17, X9, gc.NativeViewHandleCountOffset)
	f.cmpRR(X16, X17, false)
	cold(f.a.Bcond(condAE))
	f.ld64(X10, X9, gc.NativeViewHandlesOffset)
	f.a.AddShifted(X10, X10, X16, 4, false)
	f.ld32(X17, X10, gc.NativeHandleMetadataOffset)
	f.a.LsrImm(X17, X17, 16, true)
	f.a.AndImm32(X17, X17, 255)
	cold(f.a.Cbz32(X17))
	f.cmpImm(X17, gc.NativeSpaceCount-1, false)
	cold(f.a.Bcond(condA))
	f.leaDisp(X9, X9, gc.NativeViewSpacesOffset, true)
	f.a.AddShifted(X9, X9, X17, 4, false)
	f.ld32(X17, X9, gc.NativeSpaceBytesOffset)
	f.ld32(X16, X10, gc.NativeHandleOffsetOffset)
	f.cmpRR(X16, X17, false)
	cold(f.a.Bcond(condA))
	f.a.Sub32(X17, X17, X16)
	f.ld32(X16, X10, gc.NativeHandleSizeOffset)
	f.cmpRR(X16, X17, false)
	cold(f.a.Bcond(condA))
	f.cmpImm(X16, gc.HeaderSize, false)
	cold(f.a.Bcond(condB))
	f.a.MovReg32(X17, X16)
	f.ld32(X16, X10, gc.NativeHandleOffsetOffset)
	f.ld64(X9, X9, gc.NativeSpaceBaseOffset)
	f.a.Add64(X10, X9, X16)
	f.ld32(X16, X10, 4)
	f.cmpRR(X16, X17, false)
	cold(f.a.Bcond(condA))
	f.cmpImm(X16, gc.HeaderSize, false)
	cold(f.a.Bcond(condB))
	f.ld32(X16, X10, 8)
	f.ld64(X11, linMemReg, -int32(offCustomCtx))
	f.st64(X11, hcResults, X16)
	done := f.a.Branch()
	for _, site := range fallback[:n] {
		f.patchBranch19(site, f.a.Len())
	}
	f.stats.peep("gc-array-len-native")
	return done
}
