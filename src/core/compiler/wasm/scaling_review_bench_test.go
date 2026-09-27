package wasm

import (
	"fmt"
	"testing"
)

// This harness uses APIs present in beta.10 as well as the optimized version.
func BenchmarkScalingTypeAnalysis(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024} {
		for _, duplicates := range []bool{false, true} {
			b.Run(fmt.Sprintf("canonical/duplicate=%t/N=%d", duplicates, n), func(b *testing.B) {
				types := make([]RecType, n)
				for i := range types {
					idx := uint32(i)
					if duplicates {
						idx = 0
					}
					types[i].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc, Params: []ValType{RefVal(Ref(true, IndexedHeap(TypeIdx{Index: idx}), false))}}}}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for k := 0; k < b.N; k++ {
					m := Module{Types: types}
					for i := 0; i < n; i++ {
						_ = m.CanonicalTypeID(uint32(i))
					}
				}
			})
		}
		b.Run(fmt.Sprintf("subtypes/N=%d", n), func(b *testing.B) {
			types := make([]RecType, n)
			for i := range types {
				st := SubType{HasPrefix: true, Comp: CompType{Kind: CompFunc}}
				if i > 0 {
					st.Supers = []TypeIdx{{Index: uint32(i - 1)}}
				}
				types[i].SubTypes = []SubType{st}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				m := Module{Types: types}
				if _, ok := m.FunctionSubtypeTypeIndexes(0); !ok {
					b.Fatal("subtypes")
				}
			}
		})
		b.Run(fmt.Sprintf("import-index/N=%d", n), func(b *testing.B) {
			imports := make([]Import, n)
			for i := range imports {
				imports[i].Type = NewGlobalExternType(GlobalType{Type: I32})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				m := Module{Imports: imports}
				for i := 0; i < n; i++ {
					if _, ok := m.GlobalTypeByIndex(uint32(i)); !ok {
						b.Fatal("global")
					}
				}
			}
		})
		b.Run(fmt.Sprintf("recursive-keys/N=%d", n), func(b *testing.B) {
			types := []RecType{{SubTypes: make([]SubType, n)}}
			for i := range types[0].SubTypes {
				types[0].SubTypes[i] = SubType{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{RefVal(Ref(true, IndexedHeap(TypeIdx{Index: uint32((i + 1) % n), Rec: true}), false))}}}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				m := Module{Types: types}
				for i := 0; i < n; i++ {
					if _, ok := m.StructuralTypeKeyChecked(uint32(i)); !ok {
						b.Fatal("key")
					}
				}
			}
		})
	}
}
