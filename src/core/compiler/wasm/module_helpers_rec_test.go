package wasm

import "testing"

func TestResolvedTypeFuncResolvesRecursiveTypeIndexes(t *testing.T) {
	m := &Module{Types: []RecType{
		{SubTypes: []SubType{{Comp: CompType{Kind: CompStruct}}}},
		{SubTypes: []SubType{
			{Comp: CompType{Kind: CompStruct}},
			{Comp: CompType{Kind: CompFunc,
				Params:  []ValType{RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0, Rec: true}), false))},
				Results: []ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1, Rec: true}), false))},
			}},
		}},
	}}

	stored, ok := m.TypeFunc(2)
	if !ok {
		t.Fatal("TypeFunc(2) failed")
	}
	if got := stored.Params[0].Ref().Heap().Type(); !got.Rec || got.Index != 0 {
		t.Fatalf("stored param type idx = %+v, want recursive local 0", got)
	}

	resolved, ok := m.ResolvedTypeFunc(2)
	if !ok {
		t.Fatal("ResolvedTypeFunc(2) failed")
	}
	if resolved == stored {
		t.Fatal("ResolvedTypeFunc returned module storage pointer, want copy")
	}
	if got := resolved.Params[0].Ref().Heap().Type(); got.Rec || got.Index != 1 {
		t.Fatalf("resolved param type idx = %+v, want absolute 1", got)
	}
	if got := resolved.Results[0].Ref().Heap().Type(); got.Rec || got.Index != 2 {
		t.Fatalf("resolved result type idx = %+v, want absolute 2", got)
	}
	if got := stored.Params[0].Ref().Heap().Type(); !got.Rec || got.Index != 0 {
		t.Fatalf("stored signature was mutated: %+v", got)
	}
}

func TestResolveTypeFuncAliasesOnlyNonRecursiveModuleSlices(t *testing.T) {
	m := &Module{Types: []RecType{{SubTypes: []SubType{{Comp: CompType{
		Kind: CompFunc, Params: []ValType{I32}, Results: []ValType{I64},
	}}}}}}
	stored, _ := m.TypeFunc(0)
	var resolved CompType
	if !m.ResolveTypeFunc(0, &resolved) {
		t.Fatal("ResolveTypeFunc(0) failed")
	}
	if &resolved.Params[0] != &stored.Params[0] || &resolved.Results[0] != &stored.Results[0] {
		t.Fatal("non-recursive destination did not alias immutable module slices")
	}

	public, ok := m.ResolvedTypeFunc(0)
	if !ok {
		t.Fatal("ResolvedTypeFunc(0) failed")
	}
	public.Params[0] = F32
	public.Results[0] = F64
	if !EqualValType(stored.Params[0], I32) || !EqualValType(stored.Results[0], I64) {
		t.Fatal("public resolved signature mutated module storage")
	}
}

func TestResolveTypeFuncOwnsRecursiveDestinationSlices(t *testing.T) {
	m := &Module{Types: []RecType{{SubTypes: []SubType{{Comp: CompType{
		Kind:    CompFunc,
		Params:  []ValType{RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0, Rec: true}), false))},
		Results: []ValType{I32},
	}}}}}}
	stored, _ := m.TypeFunc(0)
	var resolved CompType
	if !m.ResolveTypeFunc(0, &resolved) {
		t.Fatal("ResolveTypeFunc(0) failed")
	}
	if &resolved.Params[0] == &stored.Params[0] || &resolved.Results[0] == &stored.Results[0] {
		t.Fatal("recursive destination retained module slice storage")
	}
	resolved.Params[0] = I64
	resolved.Results[0] = F64
	if got := stored.Params[0].Ref().Heap().Type(); !got.Rec {
		t.Fatal("recursive destination mutated stored params")
	}
	if !EqualValType(stored.Results[0], I32) {
		t.Fatal("recursive destination mutated stored results")
	}
}

func TestResolvedLocalFuncTypeResolvesRecursiveAndPreservesAbsoluteIndexes(t *testing.T) {
	m := &Module{
		Types: []RecType{
			{SubTypes: []SubType{{Comp: CompType{Kind: CompStruct}}}},
			{SubTypes: []SubType{
				{Comp: CompType{Kind: CompStruct}},
				{Comp: CompType{Kind: CompArray, Array: NewFieldType(StorageVal(I32), Const)}},
				{Comp: CompType{Kind: CompFunc,
					Params: []ValType{
						RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0}), false)),
						RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 1, Rec: true}), false)),
					},
				}},
			}},
		},
		FuncTypes: []TypeIdx{{Index: 3}},
	}

	resolved, ok := m.ResolvedLocalFuncType(0)
	if !ok {
		t.Fatal("ResolvedLocalFuncType(0) failed")
	}
	if got := resolved.Params[0].Ref().Heap().Type(); got.Rec || got.Index != 0 {
		t.Fatalf("absolute param type idx = %+v, want absolute 0", got)
	}
	if got := resolved.Params[1].Ref().Heap().Type(); got.Rec || got.Index != 2 {
		t.Fatalf("recursive param type idx = %+v, want absolute 2", got)
	}
}
