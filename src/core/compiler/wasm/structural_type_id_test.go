package wasm

import "testing"

func indexedRef(index uint32, nullable bool) ValType {
	return RefVal(Ref(nullable, IndexedHeap(TypeIdx{Index: index}), false))
}

func TestStructuralTypeIDIncludesIndexedReferenceStructure(t *testing.T) {
	nested := SubType{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{I32}, Results: []ValType{I64}}}
	rootA := SubType{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{indexedRef(0, true)}, Results: []ValType{indexedRef(0, false)}}}
	a := &Module{Types: []RecType{{SubTypes: []SubType{nested}}, {SubTypes: []SubType{rootA}}}}

	rootB := rootA
	rootB.Comp.Params = []ValType{indexedRef(1, true)}
	rootB.Comp.Results = []ValType{indexedRef(1, false)}
	b := &Module{Types: []RecType{
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompStruct}}}},
		{SubTypes: []SubType{nested}},
		{SubTypes: []SubType{rootB}},
	}}

	if got, want := a.StructuralTypeID(1), b.StructuralTypeID(2); got != want {
		t.Fatalf("equivalent indexed signatures have ids %#x and %#x", got, want)
	}

	mismatch := *b
	mismatch.Types = append([]RecType(nil), b.Types...)
	mismatch.Types[1].SubTypes = append([]SubType(nil), b.Types[1].SubTypes...)
	mismatch.Types[1].SubTypes[0].Comp.Params = []ValType{I64}
	if got, bad := a.StructuralTypeID(1), mismatch.StructuralTypeID(2); got == bad {
		t.Fatalf("different indexed signatures collapsed to id %#x", got)
	}
}

func TestStructuralTypeIDCanonicalizesRecursiveAndDuplicateGraphs(t *testing.T) {
	selfA := SubType{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{
		RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0, Rec: true}), false)),
	}}}
	a := &Module{Types: []RecType{{SubTypes: []SubType{selfA}}}}

	selfB := selfA
	b := &Module{Types: []RecType{
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompStruct}}}},
		{SubTypes: []SubType{selfB}},
	}}
	if got, want := a.StructuralTypeID(0), b.StructuralTypeID(1); got != want {
		t.Fatalf("equivalent recursive signatures have ids %#x and %#x", got, want)
	}

	leaf := SubType{Final: true, Comp: CompType{Kind: CompFunc, Results: []ValType{I32}}}
	shared := &Module{Types: []RecType{
		{SubTypes: []SubType{leaf}},
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{indexedRef(0, true), indexedRef(0, true)}}}}},
	}}
	duplicated := &Module{Types: []RecType{
		{SubTypes: []SubType{leaf}},
		{SubTypes: []SubType{leaf}},
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{indexedRef(0, true), indexedRef(1, true)}}}}},
	}}
	if got, want := shared.StructuralTypeID(1), duplicated.StructuralTypeID(2); got != want {
		t.Fatalf("shared and duplicated equivalent graphs have ids %#x and %#x", got, want)
	}
}
