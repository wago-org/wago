package wasm

import "testing"

func structuralBenchmarkDAG(depth int) *Module {
	m := &Module{Types: make([]RecType, depth)}
	m.Types[0].SubTypes = []SubType{{Final: true, Comp: CompType{Kind: CompFunc}}}
	for i := 1; i < depth; i++ {
		child := indexedRef(uint32(i-1), true)
		m.Types[i].SubTypes = []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{child, child}}}}
	}
	return m
}

func TestStructuralTypeKeyRepeatedQueryUsesCache(t *testing.T) {
	m := structuralBenchmarkDAG(12)
	want, ok := m.StructuralTypeKeyChecked(11)
	if !ok {
		t.Fatal("first canonicalization failed")
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		got, ok := m.StructuralTypeKeyChecked(11)
		if !ok || got != want {
			panic("cached structural identity changed")
		}
	}); allocs != 0 {
		t.Fatalf("cached structural identity allocations = %v, want 0", allocs)
	}
}

func BenchmarkStructuralTypeKey(b *testing.B) {
	b.Run("first-wide-shared-DAG", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			m := structuralBenchmarkDAG(12)
			if _, ok := m.StructuralTypeKeyChecked(11); !ok {
				b.Fatal("canonicalization failed")
			}
		}
	})
	b.Run("repeated-cached", func(b *testing.B) {
		m := structuralBenchmarkDAG(12)
		if _, ok := m.StructuralTypeKeyChecked(11); !ok {
			b.Fatal("canonicalization failed")
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, ok := m.StructuralTypeKeyChecked(11); !ok {
				b.Fatal("canonicalization failed")
			}
		}
	})
}
