//go:build amd64

package wago

import (
	"os"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

var nativeStructAllocEnabled = os.Getenv("WAGO_AMD64_NO_GC_NATIVE_ALLOC") != "1"

func (in *Instance) prepareNativeStructHandles(typeID uint32) {
	if !nativeStructAllocEnabled || in == nil || in.gc == nil || in.c == nil || int(typeID) >= len(in.c.Types) || int(typeID) >= len(in.c.GCTypeDescs) {
		return
	}
	t := in.c.Types[typeID]
	d := in.c.GCTypeDescs[typeID]
	if !t.Final || t.Kind != CompositeTypeStruct || d.Kind != gc.KindStruct || len(t.Fields) != len(d.Fields) {
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
	in.gc.PrepareNativeStructHandles()
}
