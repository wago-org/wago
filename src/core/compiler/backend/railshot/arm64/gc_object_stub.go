//go:build arm64

package arm64

import (
	"encoding/binary"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// patchGCObjectCall keeps the original helper path when a large module places
// its shared leaf outside BL range. The local fallback skips the status check
// and native load; it does not set LR or require an extra instruction at a site.
func patchGCObjectCall(a *a64.Asm, site, stub int, helperDelta uint32) bool {
	if a.PatchBranch26(site, stub) {
		return true
	}
	if helperDelta == 0 || helperDelta%4 != 0 || site < 0 || site > len(a.B)-4 || uint64(helperDelta) > uint64(len(a.B)-site-4) {
		return false
	}
	binary.LittleEndian.PutUint32(a.B[site:], 0x14000000) // B, preserving the saved caller LR
	return a.PatchBranch26(site, site+int(helperDelta))
}

func needsGCObjectStub(relocs *callRelocTable) bool {
	for i := 0; i < relocs.functions(); i++ {
		function := relocs.serialFunction(i)
		if relocs.results != nil {
			function = relocs.parallelFunction(i)
		}
		for _, rl := range function {
			if rl.gcObject {
				return true
			}
		}
	}
	return false
}

// emitGCObjectStub appends the module's checked-object leaf. Its internal ABI
// takes a full compact reference in X16 and a required byte extent in W11
// (at least HeaderSize).
// It returns X10=object and X17=0 on success, X17=1 on failure, and clobbers
// X9, X10, X16, X17 and flags. X11 and all other registers are preserved.
// The caller must already have homed caller-saved pins and saved LR. There is
// no call or safepoint here. The validator proves the operand's array kind;
// these checks prove physical access safety, without a dynamic type lookup.
func emitGCObjectStub(code []byte) []byte {
	a := a64.Asm{B: code}
	var fallback [11]int
	n := 0
	cold := func(site int) { fallback[n] = site; n++ }
	cold(a.Cbz64(X16))
	a.LsrImm(X17, X16, 32, false)
	cold(a.Cbnz64(X17))
	a.TstImm64(X16, 1)
	cold(a.Bcond(condNE))
	a.LsrImm(X16, X16, 1, true)
	a.SubImm64(X9, linMemReg, abi.GCNativeViewPtrOffset)
	a.Load64(X9, X9, 0)
	a.Load64(X9, X9, gc.NativeInstanceViewCollectorOffset)
	a.Load32(X17, X9, gc.NativeViewHandleCountOffset)
	a.CmpReg32(X16, X17)
	cold(a.Bcond(condAE))
	a.Load64(X10, X9, gc.NativeViewHandlesOffset)
	a.AddShifted(X10, X10, X16, 4, false)
	a.Load32(X17, X10, gc.NativeHandleMetadataOffset)
	a.LsrImm(X17, X17, 16, true)
	a.AndImm32(X17, X17, 255)
	cold(a.Cbz32(X17))
	a.CmpImm32(X17, gc.NativeSpaceCount-1)
	cold(a.Bcond(condA))
	a.AddImm64(X9, X9, gc.NativeViewSpacesOffset)
	a.AddShifted(X9, X9, X17, 4, false)
	a.Load32(X17, X9, gc.NativeSpaceBytesOffset)
	a.Load32(X16, X10, gc.NativeHandleOffsetOffset)
	a.CmpReg32(X16, X17)
	cold(a.Bcond(condA))
	a.Sub32(X17, X17, X16)
	a.Load32(X16, X10, gc.NativeHandleSizeOffset)
	a.CmpReg32(X16, X17)
	cold(a.Bcond(condA))
	a.CmpReg32(X16, X11)
	cold(a.Bcond(condB))
	a.MovReg32(X17, X16)
	a.Load32(X16, X10, gc.NativeHandleOffsetOffset)
	a.Load64(X9, X9, gc.NativeSpaceBaseOffset)
	a.Add64(X10, X9, X16)
	a.Load32(X16, X10, 4)
	a.CmpReg32(X16, X17)
	cold(a.Bcond(condA))
	a.CmpReg32(X16, X11)
	cold(a.Bcond(condB))
	a.MovImm32(X17, 0)
	a.Ret()
	for _, site := range fallback[:n] {
		// All branches span this fixed, bounded leaf (less than 256 bytes).
		if !a.PatchBranch19(site, a.Len()) {
			panic("arm64: checked-object leaf exceeds conditional branch range")
		}
	}
	a.MovImm32(X17, 1)
	a.Ret()
	return a.B
}
