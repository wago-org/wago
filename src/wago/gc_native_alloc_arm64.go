//go:build arm64

package wago

import (
	"os"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Only AMD64 currently publishes live structs from a reserved native batch.
const nativeLiveStructAllocSupported = false

var nativeStructAllocEnabled = os.Getenv("WAGO_ARM64_NO_GC_NATIVE_ALLOC") != "1"

func (in *Instance) prepareNativeDefaultArrayAllocation(typeID, length uint32) {
	in.prepareNativeArrayAllocation(typeID, length)
}

func (in *Instance) prepareNativeArrayAllocation(typeID, length uint32) {
	if length > 64 {
		return
	}
	if !nativeStructAllocEnabled || in == nil || in.gc == nil || in.c == nil || int(typeID) >= len(in.c.Types) || int(typeID) >= len(in.c.GCTypeDescs) {
		return
	}
	t := in.c.Types[typeID]
	d := in.c.GCTypeDescs[typeID]
	if !t.Final || t.Kind != CompositeTypeArray || d.Kind != gc.KindArray {
		return
	}
	if t.Array.Storage.Packed || (t.Array.Storage.Value.Kind != ValueTypeI32 && t.Array.Storage.Value.Kind != ValueTypeI64 && t.Array.Storage.Value.Kind != ValueTypeF32 && t.Array.Storage.Value.Kind != ValueTypeF64) {
		return
	}
	size, err := gc.ArraySize(d, length)
	if err != nil || size > gc.NativeArrayAllocMaxBytes {
		// ARM64 consumes only the shared small-array state.
		return
	}
	if in.c.genericGCBoundaryCollectionSafe() {
		in.gc.PrepareNativeArrayAllocation(size)
		return
	}
	in.gc.PrepareNativeArrayAllocationImmediate(size)
}
