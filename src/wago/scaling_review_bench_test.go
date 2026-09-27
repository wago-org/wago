package wago

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func reviewGCTypes(n int, duplicates bool) *Compiled {
	c := &Compiled{Types: make([]DefinedTypeDescriptor, n), GCTypeDescs: make([]gc.TypeDesc, n)}
	for i := range c.Types {
		// Unique signatures with O(1) metadata per type; no irrelevant payload growth.
		c.Types[i] = DefinedTypeDescriptor{RecGroup: uint32(i), Kind: CompositeTypeStruct}
		if !duplicates {
			c.Types[i].Fields = []FieldTypeDescriptor{{Storage: StorageTypeDescriptor{Value: ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(i)}}}}}}
		}
		c.GCTypeDescs[i] = gc.TypeDesc{ID: gc.TypeID(i)}
		// RecGroup members are distinguished by position for the unique workload.
		if !duplicates {
			c.Types[i].RecGroup = 0
		}
	}
	return c
}

func BenchmarkScalingGCMapping(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512} {
		for _, duplicate := range []bool{false, true} {
			c := reviewGCTypes(n, duplicate)
			b.Run(fmt.Sprintf("first/duplicate=%t/N=%d", duplicate, n), func(b *testing.B) {
				b.ReportAllocs()
				for k := 0; k < b.N; k++ {
					if _, _, _, err := gcCanonicalTypePlan(c, nil, nil, true); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(fmt.Sprintf("dedup/duplicate=%t/N=%d", duplicate, n), func(b *testing.B) {
				b.ReportAllocs()
				for k := 0; k < b.N; k++ {
					_ = hasEquivalentLocalGCHeapTypes(c)
				}
			})
		}
	}
}

func BenchmarkScalingValueIntern(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			values := make([]ValueTypeDescriptor, n)
			for i := range values {
				values[i] = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(i)}}}
			}
			// Compilation already owns a heap-allocated Compiled. Reuse that
			// container so escape analysis of this synthetic loop cannot charge
			// allocation of the entire module to just the indexed implementation.
			c := &Compiled{validateMemo: &validateMemo{}}
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				c.ValueTypes = nil
				reviewResetCompileIndexes(c)
				for _, v := range values {
					_ = c.internExactValueType(v)
				}
			}
		})
	}
}

func BenchmarkScalingReturnPCLookup(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			frame := make([]byte, 160)
			code := make([]byte, 16*(n+2))
			base := uintptr(unsafe.Pointer(&frame[0]))
			codeBase := uintptr(unsafe.Pointer(&code[0]))
			callsites := make([]compiledGCFrameCallsite, n)
			for i := range callsites {
				callsites[i] = compiledGCFrameCallsite{returnOffset: uint32(16 * (i + 1)), frameBytes: 32}
			}
			binary.LittleEndian.PutUint64(frame[40:], uint64(codeBase+uintptr(16*n)))
			binary.LittleEndian.PutUint64(frame[88:], uint64(codeBase+uintptr(16*(n+1))))
			roots := gcNativeFrameRoots{base: base, frameBytes: 32, frameLayout: gcNativeFrameLayoutARM64, codeBase: codeBase, codeBytes: uintptr(len(code)), adapterReturnOffsets: []uint32{uint32(16 * (n + 1))}, callsites: callsites}
			visit := func(gc.RootSlot) bool { return true }
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				roots.RangeRoots(visit)
			}
			runtime.KeepAlive(frame)
			runtime.KeepAlive(code)
		})
	}
}

func BenchmarkScalingCodeRangeLookup(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			collector := new(gc.Collector)
			c := &Compiled{code: make([]byte, 64), validateMemo: &validateMemo{gcFrameRoots: &compiledGCFrameRoots{}}}
			store := &referenceStore{instances: make(map[*Instance]*referenceStoreInstance)}
			for i := 0; i < n; i++ {
				in := &Instance{c: c, gc: collector, base: uintptr(0x10000 + i*8192)}
				store.instances[in] = &referenceStoreInstance{}
				store.registerGCFrameCodeRangeLocked(in)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				pc := uintptr(0x10000 + (k%n)*8192 + 32)
				if store.gcFrameOwner(pc, collector) == nil {
					b.Fatal("missing code owner")
				}
			}
		})
	}
}

func reviewResetCompileIndexes(c *Compiled) { c.validateMemo.compileIndexes = nil }

func BenchmarkScalingSparseGCMapping(b *testing.B) {
	for _, n := range []int{8, 32, 128, 512, 2048, 4096} {
		types := make([]DefinedTypeDescriptor, n)
		descs := make([]gc.TypeDesc, n)
		reps := make([]gcDomainTypeRepresentative, n)
		for i := range types {
			fields := make([]FieldTypeDescriptor, 12)
			for bit := range fields {
				fields[bit] = FieldTypeDescriptor{Mutable: i&(1<<bit) != 0, Storage: StorageTypeDescriptor{Value: ValueTypeDescriptor{Kind: ValueTypeI32}}}
			}
			types[i] = DefinedTypeDescriptor{RecGroup: uint32(i), Kind: CompositeTypeStruct, Fields: fields}
			descs[i] = gc.TypeDesc{ID: gc.TypeID(i)}
			reps[i] = gcDomainTypeRepresentative{types: types, index: uint32(i)}
		}
		c := &Compiled{Types: []DefinedTypeDescriptor{types[n-1]}, GCTypeDescs: []gc.TypeDesc{{ID: 0}}}
		b.Run(fmt.Sprintf("first/N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for k := 0; k < b.N; k++ {
				if _, _, _, err := gcCanonicalTypePlan(c, reps, descs, false); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("lookup/N=%d", n), func(b *testing.B) {
			mapping, _, _, err := gcCanonicalTypePlan(c, reps, descs, false)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				if local, ok := mapping.local(gc.TypeID(n - 1)); !ok || local != 0 {
					b.Fatal("translation")
				}
			}
			b.ReportMetric(float64(reviewMappingReverseEntries(mapping)), "retained-reverse-slots")
		})
	}
}

func reviewMappingReverseEntries(m *gcTypeMapping) int {
	return len(m.domainToLocal) + len(m.domainToLocalSparse)
}
