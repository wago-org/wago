package wasm

import (
	"fmt"
	"testing"
)

func BenchmarkScalingLocalRunLookup(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024} {
		params := []ValType{I32}
		runs := make([]LocalRun, n)
		ends := make([]uint64, n)
		for i := range runs {
			runs[i] = LocalRun{Count: 3, Type: I64}
			ends[i] = uint64(1 + 3*(i+1))
		}
		index := uint32(3 * n)
		b.Run(fmt.Sprintf("linear/N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for k := 0; k < b.N; k++ {
				if value, ok := LocalType(params, runs, index); !ok || value != I64 {
					b.Fatal("local type")
				}
			}
		})
		b.Run(fmt.Sprintf("indexed/N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for k := 0; k < b.N; k++ {
				if value, ok := LocalTypeIndexed(params, runs, ends, index); !ok || value != I64 {
					b.Fatal("local type")
				}
			}
		})
	}
}

// This harness uses APIs present in beta.10 as well as the optimized version.
func BenchmarkScalingTypeAnalysis(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024} {
		for _, duplicates := range []bool{false, true} {
			b.Run(fmt.Sprintf("canonical/duplicate=%t/N=%d", duplicates, n), func(b *testing.B) {
				types := make([]RecType, n)
				for i := range types {
					var params []ValType
					if duplicates {
						params = []ValType{I32}
					} else if i > 0 {
						// Absolute references name earlier groups, as in validated
						// modules; each signature still has constant-size metadata.
						params = []ValType{RefVal(Ref(true, IndexedHeap(TypeIdx{Index: uint32(i - 1)}), false))}
					}
					types[i].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc, Params: params}}}
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
