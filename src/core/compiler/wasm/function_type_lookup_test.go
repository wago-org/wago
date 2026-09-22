package wasm

import (
	"fmt"
	"testing"
)

func checkFunctionTypeLookupParity(t *testing.T, m *Module, lookup FunctionTypeLookup) {
	t.Helper()
	other := *m
	if len(m.Types) != 0 {
		other.Types = append([]RecType(nil), m.Types...)
		other.Types[0].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc, Params: []ValType{F32}}}}
	}
	for _, module := range []*Module{m, &other} {
		for _, candidate := range []FunctionTypeLookup{lookup, {}} {
			for index := len(m.FuncTypes); index >= -1; index-- {
				want, ok := module.LocalFuncType(index)
				got, gotOK := candidate.LocalFuncType(module, index)
				if got != want || gotOK != ok {
					t.Fatalf("groups=%d function=%d got=%p/%t want=%p/%t", len(m.Types), index, got, gotOK, want, ok)
				}
			}
		}
	}
	if got, ok := lookup.LocalFuncType(nil, 0); got != nil || ok {
		t.Fatal("nil module returned a signature")
	}
}

func TestFunctionTypeLookupParity(t *testing.T) {
	for _, groups := range []int{0, 1, 8, 9, 64, 65, 79, 80, 81, 127, 128, 129, 512, 513} {
		m := &Module{Types: make([]RecType, groups)}
		flat := uint32(0)
		for i := range m.Types {
			for j := 0; j < i%4; j++ {
				kind := CompFunc
				if j == 2 {
					kind = CompStruct
				}
				m.Types[i].SubTypes = append(m.Types[i].SubTypes, SubType{Comp: CompType{Kind: kind, Params: []ValType{I32}, Results: []ValType{I64}}})
				m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: flat})
				flat++
			}
		}
		m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: flat}, TypeIdx{Index: ^uint32(0)}, TypeIdx{Rec: true})
		lookup := NewFunctionTypeLookup(m)
		if groups > 64 && len(lookup.ends) != (groups+functionTypeLookupGroupStride-1)/functionTypeLookupGroupStride {
			t.Fatal("large parity case did not construct index")
		}
		checkFunctionTypeLookupParity(t, m, lookup)
	}
	checkFunctionTypeLookupParity(t, &Module{}, NewFunctionTypeLookup(nil))
}

func TestFunctionTypeLookupBudget(t *testing.T) {
	maxGroups := maxFunctionTypeLookupBytes / 8 * functionTypeLookupGroupStride
	types := make([]RecType, maxGroups+1)
	types[maxGroups-1].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc}}}
	m := &Module{FuncTypes: make([]TypeIdx, 9)}
	for _, groups := range []int{maxGroups, maxGroups + 1} {
		t.Run(fmt.Sprint(groups), func(t *testing.T) {
			m.Types = types[:groups]
			var lookup FunctionTypeLookup
			allocs := testing.AllocsPerRun(5, func() { lookup = NewFunctionTypeLookup(m) })
			if groups == maxGroups {
				if len(lookup.ends)*8 != maxFunctionTypeLookupBytes || allocs != 1 {
					t.Fatalf("at budget: %d offsets, %g allocations", len(lookup.ends), allocs)
				}
			} else if lookup.ends != nil || allocs != 0 {
				t.Fatal("oversize lookup allocated an index")
			}
			if got, ok := lookup.LocalFuncType(m, 0); !ok || got != &types[maxGroups-1].SubTypes[0].Comp {
				t.Fatal("lookup at the budget boundary changed the signature")
			}
		})
	}
}

func TestFunctionTypeLookupBlockBoundaries(t *testing.T) {
	for _, groups := range []int{65, 79, 80, 81, 95, 96, 97, 129} {
		for _, populated := range [][]int{nil, {16, 64}, {15, 16, 31, 32, 79, 80, 95, 96, 127, 128}} {
			m := &Module{Types: make([]RecType, groups)}
			flat := uint32(0)
			for _, group := range populated {
				if group >= groups {
					continue
				}
				for _, kind := range []CompTypeKind{CompFunc, CompStruct, CompFunc} {
					m.Types[group].SubTypes = append(m.Types[group].SubTypes, SubType{Comp: CompType{Kind: kind}})
					m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: flat})
					flat++
				}
			}
			for i := 0; i < 9; i++ {
				m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: flat + uint32(i)})
			}
			checkFunctionTypeLookupParity(t, m, NewFunctionTypeLookup(m))
		}
	}
}

func TestFunctionTypeLookupStorage(t *testing.T) {
	m := &Module{Types: make([]RecType, 4096), FuncTypes: make([]TypeIdx, 9)}
	var lookup FunctionTypeLookup
	if allocs := testing.AllocsPerRun(5, func() { lookup = NewFunctionTypeLookup(m) }); allocs != 1 {
		t.Fatalf("index used %g allocations", allocs)
	}
	if len(lookup.ends)*8 != 2048 {
		t.Fatalf("4096 groups used %d index bytes", len(lookup.ends)*8)
	}
	m.Types = m.Types[:64]
	if allocs := testing.AllocsPerRun(5, func() { lookup = NewFunctionTypeLookup(m) }); allocs != 0 {
		t.Fatal("small module allocated an index")
	}
}

func TestFunctionTypeLookupFewFunctions(t *testing.T) {
	for _, count := range []int{0, 1, 8} {
		m := &Module{Types: make([]RecType, 4096), FuncTypes: make([]TypeIdx, count)}
		var lookup FunctionTypeLookup
		allocs := testing.AllocsPerRun(5, func() { lookup = NewFunctionTypeLookup(m) })
		if lookup.ends != nil || allocs != 0 {
			t.Fatalf("functions=%d allocated index", count)
		}
	}
}

func TestFunctionTypeLookupSeparatePasses(t *testing.T) {
	m := &Module{Types: make([]RecType, 129), FuncTypes: make([]TypeIdx, 9)}
	m.Types[128].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc}}}
	first := NewFunctionTypeLookup(m)
	if got, ok := first.LocalFuncType(m, 0); !ok || got != &m.Types[128].SubTypes[0].Comp {
		t.Fatal("first pass failed")
	}
	m.Types[0].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc}}}
	second := NewFunctionTypeLookup(m)
	if got, ok := second.LocalFuncType(m, 0); !ok || got != &m.Types[0].SubTypes[0].Comp {
		t.Fatal("new pass retained old group offsets")
	}
}

func FuzzFunctionTypeLookupParity(f *testing.F) {
	for _, seed := range [][]byte{{0}, {1}, {15, 0, 1, 4}, {16, 4, 0}, {64, 1, 0, 0, 3}, {255, 2, 3, 4, 0}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		m := &Module{Types: make([]RecType, 65+int(data[0]))}
		flat := uint32(0)
		for group := range m.Types {
			value := data[group%len(data)]
			for sub := 0; sub < int(value%5); sub++ {
				kind := CompFunc
				if (int(value)+sub)%3 == 0 {
					kind = CompArray
				}
				m.Types[group].SubTypes = append(m.Types[group].SubTypes, SubType{Comp: CompType{Kind: kind}})
				m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: flat})
				flat++
			}
		}
		for i := 0; i < 9; i++ {
			m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: flat + uint32(i)})
		}
		m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: ^uint32(0)}, TypeIdx{Rec: true})
		checkFunctionTypeLookupParity(t, m, NewFunctionTypeLookup(m))
	})
}
