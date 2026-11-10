package wasm

import (
	"strconv"
	"testing"
)

func canonicalIndexBenchmarkModule(n int) *Module {
	m := &Module{Types: make([]RecType, n)}
	for i := range m.Types {
		ref := RefVal(Ref(true, IndexedHeap(TypeIdx{Index: uint32(i)}), false))
		m.Types[i].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc, Params: []ValType{ref}}}}
	}
	return m
}

func canonicalTypeIDLinear(m *Module, typeIdx uint32) uint32 {
	target, ok := m.TypeFunc(typeIdx)
	if !ok {
		return typeIdx
	}
	for j := 0; j < m.flattenedTypeCount(); j++ {
		ft, ok := m.TypeFunc(uint32(j))
		if ok && FuncTypeEqual(ft, target) {
			return uint32(j)
		}
	}
	return typeIdx
}

func BenchmarkCanonicalTypeIDs(b *testing.B) {
	for _, n := range []int{128, 512, 2048} {
		b.Run("indexed/"+strconv.Itoa(n), func(b *testing.B) {
			base := canonicalIndexBenchmarkModule(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m := *base // fresh module-local lazy indexes, same immutable type graph
				var sink uint32
				for idx := 0; idx < n; idx++ {
					sink ^= m.CanonicalTypeID(uint32(idx))
				}
				benchmarkCanonicalTypeIDSink = sink
			}
		})
		b.Run("linear-baseline/"+strconv.Itoa(n), func(b *testing.B) {
			base := canonicalIndexBenchmarkModule(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m := *base
				var sink uint32
				for idx := 0; idx < n; idx++ {
					sink ^= canonicalTypeIDLinear(&m, uint32(idx))
				}
				benchmarkCanonicalTypeIDSink = sink
			}
		})
	}
}

var benchmarkCanonicalTypeIDSink uint32

func importGlobalBenchmarkModule(n int) *Module {
	m := &Module{Imports: make([]Import, n)}
	for i := range m.Imports {
		m.Imports[i].Type = NewGlobalExternType(GlobalType{Type: I32, Mutable: i%2 == 0})
	}
	return m
}

func globalTypeByIndexLinear(m *Module, idx uint32) (GlobalType, bool) {
	globalIndex := uint32(0)
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != ExternGlobal {
			continue
		}
		if globalIndex == idx {
			return m.Imports[i].Type.GlobalType(), true
		}
		globalIndex++
	}
	local := uint64(idx) - uint64(globalIndex)
	if local >= uint64(len(m.Globals)) {
		return GlobalType{}, false
	}
	return m.Globals[int(local)].Type, true
}

func BenchmarkGlobalTypeByIndex(b *testing.B) {
	for _, n := range []int{128, 512, 2048} {
		b.Run("indexed/"+strconv.Itoa(n), func(b *testing.B) {
			base := importGlobalBenchmarkModule(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m := *base
				var sink uint32
				for idx := 0; idx < n; idx++ {
					gt, ok := m.GlobalTypeByIndex(uint32(idx))
					if ok && gt.Mutable {
						sink++
					}
				}
				benchmarkGlobalTypeSink = sink
			}
		})
		b.Run("linear-baseline/"+strconv.Itoa(n), func(b *testing.B) {
			base := importGlobalBenchmarkModule(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m := *base
				var sink uint32
				for idx := 0; idx < n; idx++ {
					gt, ok := globalTypeByIndexLinear(&m, uint32(idx))
					if ok && gt.Mutable {
						sink++
					}
				}
				benchmarkGlobalTypeSink = sink
			}
		})
	}
}

var benchmarkGlobalTypeSink uint32

func subtypeIndexBenchmarkModule(n int) *Module {
	m := &Module{Types: make([]RecType, n)}
	for i := range m.Types {
		sub := SubType{HasPrefix: true, Final: false, Comp: CompType{Kind: CompFunc}}
		if i != 0 {
			sub.Supers = []TypeIdx{{Index: uint32(i - 1)}}
		}
		m.Types[i].SubTypes = []SubType{sub}
	}
	return m
}

// functionSubtypeTypeIndexesLinear preserves the former per-candidate subtype
// walk so the benchmark shows how one result query scales on a deep chain.
func functionSubtypeTypeIndexesLinear(m *Module, target uint32) []uint32 {
	required := Ref(false, IndexedHeap(TypeIdx{Index: target}), false)
	indexes := make([]uint32, 0, 1)
	for typeIndex := 0; typeIndex < m.flattenedTypeCount(); typeIndex++ {
		if _, ok := m.TypeFunc(uint32(typeIndex)); !ok {
			continue
		}
		actual := Ref(false, IndexedHeap(TypeIdx{Index: uint32(typeIndex)}), false)
		if m.ReferenceTypeSubtype(actual, required) {
			indexes = append(indexes, uint32(typeIndex))
		}
	}
	return indexes
}

func BenchmarkFunctionSubtypeTypeIndexes(b *testing.B) {
	for _, n := range []int{128, 512, 2048} {
		b.Run("batched/"+strconv.Itoa(n), func(b *testing.B) {
			base := subtypeIndexBenchmarkModule(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m := *base // cold per-module directories, matching the first query in compilation
				indexes, ok := m.FunctionSubtypeTypeIndexes(0)
				if !ok {
					b.Fatal("subtype query returned no indexes")
				}
				benchmarkFunctionSubtypeSink = uint32(len(indexes))
			}
		})
		b.Run("per-candidate/"+strconv.Itoa(n), func(b *testing.B) {
			base := subtypeIndexBenchmarkModule(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m := *base
				benchmarkFunctionSubtypeSink = uint32(len(functionSubtypeTypeIndexesLinear(&m, 0)))
			}
		})
	}
}

var benchmarkFunctionSubtypeSink uint32
