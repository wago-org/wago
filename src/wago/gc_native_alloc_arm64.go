//go:build arm64

package wago

import (
	"os"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

var nativeStructAllocEnabled = os.Getenv("WAGO_ARM64_NO_GC_NATIVE_ALLOC") != "1"

func (in *Instance) prepareNativeStructHandles(typeID uint32) {
	if !nativeStructAllocEnabled || in == nil || in.gc == nil || in.c == nil || int(typeID) >= len(in.c.Types) || int(typeID) >= len(in.c.GCTypeDescs) {
		return
	}
	t := in.c.Types[typeID]
	d := in.c.GCTypeDescs[typeID]
	if t.Kind != CompositeTypeStruct || d.Kind != gc.KindStruct || len(t.Fields) != len(d.Fields) {
		return
	}
	slots := 1 // trailing local type index
	for i, field := range t.Fields {
		if field.Storage.Packed {
			return
		}
		switch field.Storage.Value.Kind {
		case ValueTypeI32, ValueTypeI64, ValueTypeF32, ValueTypeF64:
			slots++
		case ValueTypeV128:
			slots += 2
		case ValueTypeReference:
			heap := field.Storage.Value.Ref.Heap
			if heap.Defined || (heap.Abstract != AbstractHeapAny && heap.Abstract != AbstractHeapEq) || (d.Fields[i].Kind != gc.StorageRef && d.Fields[i].Kind != gc.StorageRefNull) {
				return
			}
			slots++
		default:
			return
		}
		if slots > 64 {
			return
		}
	}
	size, err := gc.StructSize(d)
	if err == nil {
		in.gc.PrepareNativeStructAllocation(size)
	}
}

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
	if err != nil {
		return
	}
	if in.c.genericGCBoundaryCollectionSafe() {
		in.gc.PrepareNativeArrayAllocation(size)
		return
	}
	in.gc.PrepareNativeArrayAllocationImmediate(size)
}

// Prepare only bounded batches. Larger runs generally cross a collection
// boundary and should not pay to reserve handles after every helper return.
func (in *Instance) prepareNativeDeadStructBatch(typeID, count uint32) {
	if in == nil || in.gc == nil || in.c == nil || int(typeID) >= len(in.c.GCTypeDescs) {
		return
	}
	d := in.c.GCTypeDescs[typeID]
	size, err := gc.StructSize(d)
	if err != nil {
		return
	}
	alignment := max(d.Align, uint32(8))
	stride := (uint64(size) + uint64(alignment) - 1) &^ (uint64(alignment) - 1)
	if stride == 0 || uint64(count)*stride > uint64(gc.NativeAllocationChunkBytes) {
		return
	}
	in.prepareNativeStructHandles(typeID)
}

// Dead array batches need handles but no reserved array payload chunk: the
// native shortcut charges the complete batch before the next collection.
func (in *Instance) prepareNativeDeadArrayBatch(typeID, length, count uint32) {
	if !nativeStructAllocEnabled || in == nil || in.gc == nil || in.c == nil || int(typeID) >= len(in.c.GCTypeDescs) {
		return
	}
	d := in.c.GCTypeDescs[typeID]
	size, err := gc.ArraySize(d, length)
	if err != nil || !d.PointerFree() || length > 64 || uint64(count)*uint64(size) > uint64(gc.NativeAllocationChunkBytes) {
		return
	}
	in.gc.PrepareNativeStructAllocation(size)
}

func (in *Instance) prepareNativeDeadStructHandle(typeID uint32) {
	in.prepareNativeStructHandles(typeID)
}
