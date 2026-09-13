package wasm

import "testing"

func TestFunctionTypeLookupParity(t *testing.T) {
	for _, groups := range []int{0, 1, 8, 9, 64, 129, 512} {
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
		m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: flat + 1}, TypeIdx{Rec: true})
		lookup := NewFunctionTypeLookup(m)
		if groups > 64 && len(lookup.ends) != groups {
			t.Fatal("large parity case did not construct index")
		}
		copyModule := *m
		for _, module := range []*Module{m, &copyModule} {
			for index := -1; index <= len(m.FuncTypes); index++ {
				want, ok := module.LocalFuncType(index)
				got, gotOK := lookup.LocalFuncType(module, index)
				if got != want || gotOK != ok {
					t.Fatalf("groups=%d function=%d got=%p/%t want=%p/%t", groups, index, got, gotOK, want, ok)
				}
			}
		}
	}
}

func TestFunctionTypeLookupBudget(t *testing.T) {
	m := &Module{Types: make([]RecType, maxFunctionTypeLookupBytes/8+1)}
	m.Types[len(m.Types)-1].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc}}}
	m.FuncTypes = make([]TypeIdx, 9)
	lookup := NewFunctionTypeLookup(m)
	if lookup.ends != nil {
		t.Fatal("oversize lookup allocated an index")
	}
	if got, ok := lookup.LocalFuncType(m, 0); !ok || got != &m.Types[len(m.Types)-1].SubTypes[0].Comp {
		t.Fatal("budget fallback changed lookup")
	}
}

func TestFunctionTypeLookupFewFunctions(t *testing.T) {
	for _, count := range []int{0, 1, 8} {
		m := &Module{Types: make([]RecType, 4096), FuncTypes: make([]TypeIdx, count)}
		if lookup := NewFunctionTypeLookup(m); lookup.ends != nil {
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
