package gc

import (
	"encoding/binary"
	"testing"
)

var objectAccessBenchSink uint64

// checkedPrototypeBytes models the minimum metadata walk a direct JIT object
// access would still need with compact moving references: validate the handle,
// select its current space, bounds-check the entry, and prove the exact runtime
// type before touching payload bytes. It deliberately uses no unsafe pointer and
// is benchmark-only; production metadata remains collector-private.
func checkedPrototypeBytes(c *Collector, ref Ref, expected TypeID) ([]byte, bool) {
	if c == nil || c.closed || !ref.IsObj() {
		return nil, false
	}
	h := handleOf(ref)
	if h == 0 || int(h) >= len(c.handles) {
		return nil, false
	}
	e := c.handles[h]
	if e.space == spaceFree || e.size < HeaderSize {
		return nil, false
	}
	var heap []byte
	switch e.space {
	case spaceNursery:
		heap = c.nursery
	case spaceOld, spaceLarge:
		heap = c.throughput.mem
	case spaceTiny:
		heap = c.tiny.mem
	default:
		return nil, false
	}
	end := uint64(e.off) + uint64(e.size)
	if end > uint64(len(heap)) {
		return nil, false
	}
	object := heap[e.off:uint32(end)]
	if TypeID(binary.LittleEndian.Uint32(object)) != expected {
		return nil, false
	}
	return object, true
}

func BenchmarkGCSubtypeInterval(b *testing.B) {
	for _, depth := range []int{1, 16, 256} {
		types := make([]TypeDesc, depth+1)
		for i := range types {
			types[i], _ = NewStructDesc(TypeID(i), []StorageKind{StorageI32})
			types[i].Final = false
			if i > 0 {
				types[i].HasSuper, types[i].Super = true, TypeID(i-1)
			}
		}
		c, err := NewCollector(Config{}, types)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(c.Close)
		actual := TypeID(depth)
		b.Run("interval/depth="+benchmarkLength(uint32(depth)), func(b *testing.B) {
			saved := gcSubtypeIntervalsEnabled
			gcSubtypeIntervalsEnabled = true
			defer func() { gcSubtypeIntervalsEnabled = saved }()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				matched, err := c.TypeSubtype(actual, 0)
				if err != nil || !matched {
					b.Fatalf("subtype = %v, %v", matched, err)
				}
			}
		})
		b.Run("parent-chain/depth="+benchmarkLength(uint32(depth)), func(b *testing.B) {
			saved := gcSubtypeIntervalsEnabled
			gcSubtypeIntervalsEnabled = false
			defer func() { gcSubtypeIntervalsEnabled = saved }()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				matched, err := c.TypeSubtype(actual, 0)
				if err != nil || !matched {
					b.Fatalf("subtype = %v, %v", matched, err)
				}
			}
		})
	}
}

func BenchmarkTypedGCStructAccess(b *testing.B) {
	base, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		b.Fatal(err)
	}
	base.Final = false
	child, err := NewStructDesc(1, []StorageKind{StorageI32})
	if err != nil {
		b.Fatal(err)
	}
	child.HasSuper = true
	child.Super = 0
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, DisableCollection: true}, []TypeDesc{base, child})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	ref, err := c.NewStructDefault(1)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("get_separate_type_and_access", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			actual, err := c.ObjectType(ref)
			if err != nil {
				b.Fatal(err)
			}
			matched, err := c.TypeSubtype(actual, 0)
			if err != nil || !matched {
				b.Fatalf("subtype = %v, %v", matched, err)
			}
			value, err := c.StructGet(ref, 0)
			if err != nil {
				b.Fatal(err)
			}
			objectAccessBenchSink = value.Bits
		}
	})
	b.Run("get_combined_typed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, _, matched, err := c.StructGetTyped(ref, 0, false, 0)
			if err != nil || !matched {
				b.Fatalf("typed get = %v, %v", matched, err)
			}
			objectAccessBenchSink = value.Bits
		}
	})
	b.Run("set_separate_type_and_access", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			actual, err := c.ObjectType(ref)
			if err != nil {
				b.Fatal(err)
			}
			matched, err := c.TypeSubtype(actual, 0)
			if err != nil || !matched {
				b.Fatalf("subtype = %v, %v", matched, err)
			}
			if err := c.StructSet(ref, 0, I32Value(int32(i))); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("set_combined_typed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, matched, err := c.StructSetTyped(ref, 0, false, 0, I32Value(int32(i)))
			if err != nil || !matched {
				b.Fatalf("typed set = %v, %v", matched, err)
			}
		}
	})
}

func BenchmarkFinalGCStructReferenceGet(b *testing.B) {
	desc, err := NewStructDesc(0, []StorageKind{StorageRefNull})
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, DisableCollection: true}, []TypeDesc{desc})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	ref, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("combined_typed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, _, matched, err := c.StructGetTyped(ref, 0, true, 0)
			if err != nil || !matched {
				b.Fatalf("typed get = %v, %v", matched, err)
			}
			objectAccessBenchSink = uint64(value.Ref)
		}
	})
	b.Run("final_reference", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, matched, err := c.StructGetFinalRef(ref, 0, 0)
			if err != nil || !matched {
				b.Fatalf("final ref get = %v, %v", matched, err)
			}
			objectAccessBenchSink = uint64(value)
		}
	})
}

func BenchmarkTypedGCArrayAccess(b *testing.B) {
	base, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		b.Fatal(err)
	}
	base.Final = false
	child, err := NewArrayDesc(1, StorageI32)
	if err != nil {
		b.Fatal(err)
	}
	child.HasSuper = true
	child.Super = 0
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, DisableCollection: true}, []TypeDesc{base, child})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	ref, err := c.NewArrayDefault(1, 8)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("len_separate_type_and_access", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			actual, err := c.ObjectType(ref)
			if err != nil {
				b.Fatal(err)
			}
			matched, err := c.TypeSubtype(actual, 0)
			if err != nil || !matched {
				b.Fatalf("subtype = %v, %v", matched, err)
			}
			length, err := c.ArrayLen(ref)
			if err != nil {
				b.Fatal(err)
			}
			objectAccessBenchSink = uint64(length)
		}
	})
	b.Run("len_combined_typed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			length, _, matched, err := c.ArrayLenTyped(ref, 0, false)
			if err != nil || !matched {
				b.Fatalf("typed len = %v, %v", matched, err)
			}
			objectAccessBenchSink = uint64(length)
		}
	})
	b.Run("get_separate_type_and_access", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			actual, err := c.ObjectType(ref)
			if err != nil {
				b.Fatal(err)
			}
			matched, err := c.TypeSubtype(actual, 0)
			if err != nil || !matched {
				b.Fatalf("subtype = %v, %v", matched, err)
			}
			value, err := c.ArrayGet(ref, 3)
			if err != nil {
				b.Fatal(err)
			}
			objectAccessBenchSink = value.Bits
		}
	})
	b.Run("get_combined_typed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, _, matched, err := c.ArrayGetTyped(ref, 0, false, 3)
			if err != nil || !matched {
				b.Fatalf("typed get = %v, %v", matched, err)
			}
			objectAccessBenchSink = value.Bits
		}
	})
	b.Run("set_separate_type_and_access", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			actual, err := c.ObjectType(ref)
			if err != nil {
				b.Fatal(err)
			}
			matched, err := c.TypeSubtype(actual, 0)
			if err != nil || !matched {
				b.Fatalf("subtype = %v, %v", matched, err)
			}
			if err := c.ArraySet(ref, 3, I32Value(int32(i))); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("set_combined_typed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, matched, err := c.ArraySetTyped(ref, 0, false, 3, I32Value(int32(i)))
			if err != nil || !matched {
				b.Fatalf("typed set = %v, %v", matched, err)
			}
		}
	})
}

func BenchmarkCheckedGCObjectAccessPrototype(b *testing.B) {
	structDesc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		b.Fatal(err)
	}
	arrayDesc, err := NewArrayDesc(1, StorageI32)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20, DisableCollection: true}, []TypeDesc{structDesc, arrayDesc})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	structRef, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	arrayRef, err := c.NewArrayDefault(1, 64)
	if err != nil {
		b.Fatal(err)
	}
	if err := c.StructSet(structRef, 0, I32Value(7)); err != nil {
		b.Fatal(err)
	}
	if err := c.ArraySet(arrayRef, 31, I32Value(9)); err != nil {
		b.Fatal(err)
	}
	fieldOffset := uint64(PayloadOffset + structDesc.Fields[0].Offset)
	arrayOffset := uint64(PayloadOffset) + 31*uint64(arrayDesc.ElemSize)

	b.Run("struct_get_api", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, err := c.StructGet(structRef, 0)
			if err != nil {
				b.Fatal(err)
			}
			objectAccessBenchSink = value.Bits
		}
	})
	b.Run("struct_get_checked_prototype", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			object, ok := checkedPrototypeBytes(c, structRef, 0)
			if !ok || fieldOffset+4 > uint64(len(object)) {
				b.Fatal("checked struct access rejected")
			}
			objectAccessBenchSink = uint64(binary.LittleEndian.Uint32(object[fieldOffset:]))
		}
	})
	b.Run("struct_set_api", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := c.StructSet(structRef, 0, I32Value(int32(i))); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("struct_set_checked_prototype", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			object, ok := checkedPrototypeBytes(c, structRef, 0)
			if !ok || fieldOffset+4 > uint64(len(object)) {
				b.Fatal("checked struct access rejected")
			}
			binary.LittleEndian.PutUint32(object[fieldOffset:], uint32(i))
		}
	})
	b.Run("array_get_api", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, err := c.ArrayGet(arrayRef, 31)
			if err != nil {
				b.Fatal(err)
			}
			objectAccessBenchSink = value.Bits
		}
	})
	b.Run("array_get_checked_prototype", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			object, ok := checkedPrototypeBytes(c, arrayRef, 1)
			if !ok || binary.LittleEndian.Uint32(object[8:]) <= 31 || arrayOffset+4 > uint64(len(object)) {
				b.Fatal("checked array access rejected")
			}
			objectAccessBenchSink = uint64(binary.LittleEndian.Uint32(object[arrayOffset:]))
		}
	})
	b.Run("array_set_api", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := c.ArraySet(arrayRef, 31, I32Value(int32(i))); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("array_set_checked_prototype", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			object, ok := checkedPrototypeBytes(c, arrayRef, 1)
			if !ok || binary.LittleEndian.Uint32(object[8:]) <= 31 || arrayOffset+4 > uint64(len(object)) {
				b.Fatal("checked array access rejected")
			}
			binary.LittleEndian.PutUint32(object[arrayOffset:], uint32(i))
		}
	})
}
