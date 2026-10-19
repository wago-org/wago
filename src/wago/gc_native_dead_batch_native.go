//go:build amd64 || arm64

package wago

import "github.com/wago-org/wago/src/core/runtime/gc/native"

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
	in.prepareNativeDeadStructHandle(typeID)
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
	in.gc.PrepareNativeDeadStructAllocation(size)
}
