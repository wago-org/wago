package wasm

import (
	"fmt"
	"testing"
)

func checkCanonicalIndexAgainstLinear(t *testing.T, m *Module) {
	t.Helper()
	var flat []*CompType
	for i := range m.Types {
		for j := range m.Types[i].SubTypes {
			flat = append(flat, &m.Types[i].SubTypes[j].Comp)
		}
	}
	for i, target := range flat {
		want := uint32(i)
		if target.Kind == CompFunc {
			for j, candidate := range flat {
				if candidate.Kind == CompFunc && FuncTypeEqual(candidate, target) {
					want = uint32(j)
					break
				}
			}
		}
		if got := m.CanonicalTypeID(uint32(i)); got != want {
			t.Fatalf("type %d: indexed ID %d, linear ID %d", i, got, want)
		}
	}
}

func TestCanonicalIndexMatchesSemanticSignatures(t *testing.T) {
	for _, count := range []int{4, 16, 128} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			m := &Module{Types: []RecType{
				{SubTypes: []SubType{{Comp: CompType{Kind: CompStruct}}}},
				{SubTypes: []SubType{{Comp: CompType{Kind: CompStruct}}}},
			}}
			values := []ValType{
				I32, I64, FuncRef, RefVal(Ref(true, AbsHeap(HeapFunc), false)),
				RefVal(Ref(false, AbsHeap(HeapFunc), false)),
				RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0}), false)),
				RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 1}), false)),
				RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0}), true)),
				RefVal(Ref(true, DefinedHeap(&DefType{Rec: m.Types[0], GroupIndex: 0}), false)),
				RefVal(Ref(true, DefinedHeap(&DefType{Rec: m.Types[1], GroupIndex: 1}), false)),
				RefVal(Ref(true, DefinedHeap(nil), false)),
			}
			for i := 0; i < count; i++ {
				comp := CompType{Kind: CompFunc, Params: []ValType{values[i%len(values)]}}
				if i%3 == 0 {
					comp.Results = []ValType{I32}
				}
				m.Types = append(m.Types, RecType{SubTypes: []SubType{{Comp: comp}}})
			}
			checkCanonicalIndexAgainstLinear(t, m)
		})
	}
}

func TestCanonicalIndexRefreshesAfterRevalidation(t *testing.T) {
	m := &Module{Types: make([]RecType, 12)}
	for i := range m.Types {
		m.Types[i].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc, Params: []ValType{I32}}}}
	}
	checkCanonicalIndexAgainstLinear(t, m)
	// Neither edit replaces or changes the length of the outer type slice.
	m.Types[0].SubTypes[0].Comp.Params[0] = I64
	m.Types[11].SubTypes = append(m.Types[11].SubTypes, SubType{Comp: CompType{Kind: CompFunc, Params: []ValType{I64}}})
	if err := ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	checkCanonicalIndexAgainstLinear(t, m)
}

func TestCanonicalIndexDuplicatePrefixMatchesLinear(t *testing.T) {
	for _, count := range []int{16, 64} {
		for _, uniform := range []bool{true, false} {
			t.Run(fmt.Sprintf("types=%d/uniform=%t", count, uniform), func(t *testing.T) {
				m := &Module{Types: make([]RecType, count)}
				for i := range m.Types {
					comp := CompType{Kind: CompFunc, Params: []ValType{I32}}
					if !uniform {
						switch i {
						case count / 2:
							comp.Params = []ValType{I64}
						case count/2 + 1:
							comp = CompType{Kind: CompStruct}
						case count/2 + 2:
							comp.Params = []ValType{FuncRef}
						case count/2 + 3:
							comp.Params = []ValType{RefVal(Ref(true, AbsHeap(HeapFunc), false))}
						}
					}
					m.Types[i] = RecType{SubTypes: []SubType{{Comp: comp}}}
				}
				checkCanonicalIndexAgainstLinear(t, m)
				checkCanonicalIndexAgainstLinear(t, m) // Reusing published IDs preserves the proof.
			})
		}
	}
}
