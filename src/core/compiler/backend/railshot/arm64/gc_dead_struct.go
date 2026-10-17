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

// deadStructBatchShortcut charges a proven-dead batch only when the complete
// charge fits before the next collection boundary. The slow helper retains
// stress, failure injection, OOM, and collection behavior.
func (f *fn) deadStructBatchShortcut(typeIndex uint32, countSlot int) int {
	if !gc.NativeDeadBatchSupported {
		return -1
	}
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
	if !ok || !layout.NativeAlloc || !layout.PointerFree || layout.ObjectSize > 4095 {
		return -1
	}
	return f.deadObjectCountShortcut(layout.ObjectSize, max(layout.Align, 8), countSlot)
}

// deadObjectShortcut retains the allocation's byte charge and counters even
// when a proven scalar consumer makes its identity and payload unobservable.
func (f *fn) deadObjectShortcut(size, align uint32) int {
	return f.deadObjectCountShortcut(size, align, -1)
}

func (f *fn) deadObjectCountShortcut(size, align uint32, countSlot int) int {
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
	if countSlot >= 0 {
		// Array reservations may contain unused space. Let the helper cancel
		// those before charging a batch, preserving collection boundaries.
		f.ld32(X17, X10, gc.NativeStructAllocChunkEndOffset)
		cold(f.a.Cbnz32(X17))
	}
	f.ld64(X10, X9, gc.NativeViewNurseryBumpOffset)
	cold(f.a.Cbz64(X10))
	if countSlot >= 0 {
		f.ld32(X16, X10, 0)
		f.a.AddImm64(X16, X16, align-1)
		f.a.AndImm64(X16, X16, ^uint64(align-1))
		f.ld32(X11, SP, f.spillOff(countSlot))
		cold(f.a.Cbz32(X11))
		f.a.SubImm64(X11, X11, 1)
		stride := (size + align - 1) & ^(align - 1)
		f.a.MovImm64(X17, uint64(stride))
		f.a.Mul64(X11, X11, X17)
		f.a.AddImm64(X11, X11, size)
		f.ld32(X17, X9, gc.NativeViewNurseryAllocBytesOffset)
		f.cmpRR(X16, X17, true)
		cold(f.a.Bcond(condA))
		f.a.Sub64(X17, X17, X16)
		f.cmpRR(X11, X17, true)
		cold(f.a.Bcond(condA))
		f.ld64(X9, X9, gc.NativeViewAllocationCountOffset)
		cold(f.a.Cbz64(X9))
		// Every guard precedes the first mutation, and the bounded charge
		// proves the final bump fits in its uint32 field.
		f.a.Add32(X16, X16, X11)
		f.st32(X10, 0, X16)
		f.ld32(X17, SP, f.spillOff(countSlot))
		f.ld64(X16, X9, 0)
		f.a.Add64(X16, X16, X17)
		f.st64(X9, 0, X16)
		f.a.MovReg64(X11, X9)
	} else {
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
	}
	// Count is nonzero only for an active reservation. Its publication
	// normalized this counter's cycle; every collection cancels the reservation.
	f.ld32(X16, X11, gc.NativeAllocationDeadObjectsOffset)
	if countSlot >= 0 {
		f.a.Add32(X16, X16, X17)
	} else {
		f.a.AddImm32(X16, X16, 1)
	}
	f.st32(X11, gc.NativeAllocationDeadObjectsOffset, X16)
	f.ld64(X11, linMemReg, -int32(offCustomCtx))
	f.st64(X11, hcResults, ZR)
	done := f.a.Branch()
	for _, site := range fallback {
		f.patchBranch19(site, f.a.Len())
	}
	if countSlot >= 0 {
		f.stats.peep("gc-native-batch-charge")
	} else {
		f.stats.peep("gc-constructor-native-charge")
	}
	return done
}
