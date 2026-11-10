package wasm

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// The groups agree while the comparison is recursive, then the first member's
// payload disproves equivalence. A later wrapper compares a provisionally equal
// member from that failed group. Shared state must agree with independent roots.
func comparisonRollbackModule(kind CompTypeKind) *Module {
	ref := func(i uint32, rec bool) ValType {
		return RefVal(Ref(true, IndexedHeap(TypeIdx{Index: i, Rec: rec}), false))
	}
	group := func(scalar ValType) RecType {
		first := CompType{Kind: kind}
		switch kind {
		case CompFunc:
			first.Params = []ValType{ref(1, true), scalar}
		case CompStruct:
			first.Fields = []FieldType{NewFieldType(StorageVal(ref(1, true)), Const), NewFieldType(StorageVal(scalar), Const)}
		case CompArray:
			first.Array = NewFieldType(StorageVal(scalar), Const)
		}
		return RecType{SubTypes: []SubType{
			{Comp: first},
			{Comp: CompType{Kind: CompFunc, Params: []ValType{ref(0, true)}}},
		}}
	}
	wrapper := func(a, b uint32) RecType {
		return RecType{SubTypes: []SubType{{Comp: CompType{Kind: CompFunc, Params: []ValType{ref(a, false), ref(b, false)}}}}}
	}
	return &Module{Types: []RecType{group(I32), group(I64), wrapper(0, 1), wrapper(2, 1), wrapper(0, 3)}}
}

func TestRecursiveComparisonRollsBackProvisionalSuccess(t *testing.T) {
	for _, kind := range []CompTypeKind{CompFunc, CompStruct, CompArray} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			m := comparisonRollbackModule(kind)
			v := &moduleValidator{m: m}
			state := make(map[moduleTypePair]uint8)
			for _, pair := range [][2]uint32{{5, 4}, {6, 4}, {2, 0}, {3, 1}, {4, 4}} {
				a, b := TypeIdx{Index: pair[0]}, TypeIdx{Index: pair[1]}
				want := v.typeIdxEquivalent(a, b)
				if got := v.typeIdxEquivalentWithState(a, b, state); got != want {
					t.Fatalf("pair %v: batch=%v independent=%v", pair, got, want)
				}
			}
			got, ok := m.FunctionSubtypeTypeIndexes(4)
			want := functionSubtypeTypeIndexesLinear(m, 4)
			if !ok || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(got, []uint32{4}) {
				t.Fatalf("subtypes: batch=%v independent=%v", got, want)
			}
		})
	}
}

func FuzzBatchedRecursiveComparison(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{2, 2, 2, 2, 2})
	f.Fuzz(func(t *testing.T, choices []byte) {
		if len(choices) == 0 {
			return
		}
		m := comparisonRollbackModule([]CompTypeKind{CompFunc, CompStruct, CompArray}[choices[0]%3])
		// Include declared supers and duplicate wrappers; all references point to
		// earlier groups, preserving the group-order precondition of the indexes.
		for i, c := range choices[1:] {
			if i == 16 {
				break
			}
			st := SubType{Comp: CompType{Kind: CompFunc}}
			if c&1 != 0 {
				st.Supers = []TypeIdx{{Index: 4}}
				st.Comp = m.Types[2].SubTypes[0].Comp
			}
			m.Types = append(m.Types, RecType{SubTypes: []SubType{st}})
		}
		n := m.flattenedTypeCount()
		v := &moduleValidator{m: m}
		shared := make(map[moduleTypePair]uint8)
		for i := 0; i < n; i++ {
			for j := n - 1; j >= 0; j-- {
				a, b := TypeIdx{Index: uint32(i)}, TypeIdx{Index: uint32(j)}
				if got, want := v.typeIdxEquivalentWithState(a, b, shared), v.typeIdxEquivalent(a, b); got != want {
					t.Fatalf("%d,%d batch=%v independent=%v", i, j, got, want)
				}
			}
			if _, ok := m.TypeFunc(uint32(i)); ok {
				got, _ := m.FunctionSubtypeTypeIndexes(uint32(i))
				want := functionSubtypeTypeIndexesLinear(m, uint32(i))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("target %d batch=%v independent=%v", i, got, want)
				}
			}
		}
	})
}

func TestTinyFunctionSubtypeSelectionMatchesIndependentRelation(t *testing.T) {
	fn := SubType{Comp: CompType{Kind: CompFunc, Params: []ValType{I32}}}
	other := SubType{Comp: CompType{Kind: CompFunc, Params: []ValType{I64}}}
	structure := SubType{Comp: CompType{Kind: CompStruct}}
	child := fn
	child.Supers = []TypeIdx{{Index: 0}}
	ref := func(member uint32) SubType {
		return SubType{Comp: CompType{Kind: CompFunc, Params: []ValType{RefVal(Ref(true, IndexedHeap(TypeIdx{Index: member, Rec: true}), false))}}}
	}
	for name, types := range map[string][]RecType{
		"singleton":           {{SubTypes: []SubType{fn}}},
		"recursive-singleton": {{SubTypes: []SubType{ref(0)}}},
		"matching":            {{SubTypes: []SubType{fn}}, {SubTypes: []SubType{fn}}},
		"different":           {{SubTypes: []SubType{fn}}, {SubTypes: []SubType{other}}},
		"super":               {{SubTypes: []SubType{fn}}, {SubTypes: []SubType{child}}},
		"mixed":               {{SubTypes: []SubType{structure}}, {SubTypes: []SubType{fn}}},
		"mutual-recursion":    {{SubTypes: []SubType{ref(1), ref(0)}}},
	} {
		t.Run(name, func(t *testing.T) {
			m := &Module{Types: types}
			for target := uint32(0); target < 3; target++ {
				got, ok := m.FunctionSubtypeTypeIndexes(target)
				want := functionSubtypeTypeIndexesLinear(m, target)
				if !slices.Equal(got, want) || ok != (len(want) != 0) {
					t.Fatalf("target %d: got %v/%t, independent %v", target, got, ok, want)
				}
			}
		})
	}
}
