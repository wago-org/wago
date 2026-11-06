//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// deadStructShortcut charges the complete object size for a constructor whose
// only consumer is a forwarded scalar read. Caller homes and initializer values
// have already been published. It uses only the host transition's scratch bank.
func (f *fn) deadStructShortcut(typeIndex uint32) int {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
	if !ok || !layout.NativeAlloc || !layout.PointerFree || layout.ObjectSize > 4095 {
		return -1
	}
	return f.deadObjectShortcut(layout.ObjectSize, max(layout.Align, 8))
}

// deadObjectShortcut retains the allocation's byte charge and counters even
// when a proven scalar consumer makes its identity and payload unobservable.
func (f *fn) deadObjectShortcut(size, align uint32) int {
	if align > 4096 {
		return -1
	}
	var fallback []int
	cold := func(site int) { fallback = append(fallback, site) }
	f.ld64(X9, linMemReg, -int32(abi.GCNativeViewPtrOffset))
	cold(f.a.Cbz64(X9))
	f.ld64(X9, X9, gc.NativeInstanceViewCollectorOffset)
	cold(f.a.Cbz64(X9))
	f.ld32(X16, X9, gc.NativeViewNurseryObjectMaxBytesOffset)
	f.cmpImm(X16, size, false)
	cold(f.a.Bcond(condBE))
	f.ld64(X10, X9, gc.NativeViewStructAllocStateOffset)
	cold(f.a.Cbz64(X10))
	f.ld32(X16, X10, gc.NativeStructAllocCountOffset)
	cold(f.a.Cbz32(X16))
	f.cmpImm(X16, gc.NativeStructAllocHandleCapacity, false)
	cold(f.a.Bcond(condA))
	f.ld64(X10, X9, gc.NativeViewNurseryBumpOffset)
	cold(f.a.Cbz64(X10))
	f.ld32(X16, X10, 0)
	f.a.AddImm32(X16, X16, align-1)
	f.a.AndImm32(X16, X16, ^(align - 1))
	f.ld32(X17, X9, gc.NativeViewNurseryAllocBytesOffset)
	f.cmpRR(X16, X17, false)
	cold(f.a.Bcond(condA))
	f.a.Sub32(X17, X17, X16)
	f.cmpImm(X17, size, false)
	cold(f.a.Bcond(condB))
	f.ld64(X11, X9, gc.NativeViewAllocationCountOffset)
	cold(f.a.Cbz64(X11))
	// No state changes precede the final fallback check.
	f.a.AddImm32(X16, X16, size)
	f.st32(X10, 0, X16)
	f.ld64(X16, X11, 0)
	f.a.AddImm64(X16, X16, 1)
	f.st64(X11, 0, X16)
	// The active reservation normalized this counter's cycle; every collection
	// cancels the reservation.
	f.ld32(X16, X11, gc.NativeAllocationDeadObjectsOffset)
	f.a.AddImm32(X16, X16, 1)
	f.st32(X11, gc.NativeAllocationDeadObjectsOffset, X16)
	f.ld64(X11, linMemReg, -int32(offCustomCtx))
	f.st64(X11, hcResults, ZR)
	done := f.a.Branch()
	for _, site := range fallback {
		f.patchBranch19(site, f.a.Len())
	}
	f.stats.peep("gc-constructor-native-charge")
	return done
}
