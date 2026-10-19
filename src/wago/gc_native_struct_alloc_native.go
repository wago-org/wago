//go:build amd64 || arm64

package wago

import "github.com/wago-org/wago/src/core/runtime/gc/native"

func (in *Instance) prepareNativeStructHandles(typeID uint32) {
	in.prepareNativeStructHandlesMode(typeID, false)
}

func (in *Instance) prepareNativeDeadStructHandle(typeID uint32) {
	in.prepareNativeStructHandlesMode(typeID, true)
}

func (in *Instance) prepareNativeStructHandlesMode(typeID uint32, dead bool) {
	if !dead && !nativeLiveStructAllocSupported {
		return
	}
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
		if dead {
			in.gc.PrepareNativeDeadStructAllocation(size)
		} else {
			in.gc.PrepareNativeStructAllocation(size)
		}
	}
}
