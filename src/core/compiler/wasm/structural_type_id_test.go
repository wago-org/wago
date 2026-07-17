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
	if got, want := a.StructuralTypeKey(1), b.StructuralTypeKey(2); got != want {
		t.Fatalf("equivalent indexed signatures have keys %#x and %#x", got, want)
	}

	mismatch := *b
	mismatch.Types = append([]RecType(nil), b.Types...)
	mismatch.Types[1].SubTypes = append([]SubType(nil), b.Types[1].SubTypes...)
	mismatch.Types[1].SubTypes[0].Comp.Params = []ValType{I64}
	if got, bad := a.StructuralTypeID(1), mismatch.StructuralTypeID(2); got == bad {
		t.Fatalf("different indexed signatures collapsed to id %#x", got)
	}
	if got, bad := a.StructuralTypeKey(1), mismatch.StructuralTypeKey(2); got == bad {
		t.Fatalf("different indexed signatures collapsed to key %#x", got)
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
	if got, want := a.StructuralTypeKey(0), b.StructuralTypeKey(1); got != want {
		t.Fatalf("equivalent recursive signatures have keys %#x and %#x", got, want)
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
	if got, want := shared.StructuralTypeKey(1), duplicated.StructuralTypeKey(2); got != want {
		t.Fatalf("shared and duplicated equivalent graphs have keys %#x and %#x", got, want)
	}
}

func TestStructuralTypeKeyIncludesWholeRecursiveGroup(t *testing.T) {
	emptyFunc := SubType{Final: true, Comp: CompType{Kind: CompFunc}}
	emptyStruct := SubType{Final: true, Comp: CompType{Kind: CompStruct}}
	a := &Module{Types: []RecType{{SubTypes: []SubType{emptyFunc, emptyStruct}}}}
	b := &Module{Types: []RecType{
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompStruct}}}},
		{SubTypes: []SubType{emptyFunc, emptyStruct}},
	}}
	if got, want := a.StructuralTypeKey(0), b.StructuralTypeKey(1); got != want {
		t.Fatalf("equivalent whole recursive groups have keys %#x and %#x", got, want)
	}

	reordered := &Module{Types: []RecType{{SubTypes: []SubType{emptyStruct, emptyFunc}}}}
	if got, bad := a.StructuralTypeKey(0), reordered.StructuralTypeKey(1); got == bad {
		t.Fatalf("recursive group order/member position collapsed to key %#x", got)
	}
	singleton := &Module{Types: []RecType{{SubTypes: []SubType{emptyFunc}}}}
	if got, bad := a.StructuralTypeKey(0), singleton.StructuralTypeKey(0); got == bad {
		t.Fatalf("non-singleton recursive group collapsed to singleton key %#x", got)
	}

	selfField := FieldType{Storage: StorageType{Val: RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0, Rec: true}), false))}}
	selfGroup := RecType{SubTypes: []SubType{emptyFunc, {Final: true, Comp: CompType{Kind: CompStruct, Fields: []FieldType{selfField}}}}}
	externalField := FieldType{Storage: StorageType{Val: indexedRef(0, false)}}
	externalGroup := RecType{SubTypes: []SubType{emptyFunc, {Final: true, Comp: CompType{Kind: CompStruct, Fields: []FieldType{externalField}}}}}
	linked := &Module{Types: []RecType{selfGroup, externalGroup}}
	if got, bad := linked.StructuralTypeKey(0), linked.StructuralTypeKey(2); got == bad {
		t.Fatalf("self-recursive and externally linked groups collapsed to key %#x", got)
	}
}

func TestStructuralTypeKeyCanonicalizationScalesAndRemainsBounded(t *testing.T) {
	graph := func(depth int) *Module {
		m := &Module{Types: make([]RecType, depth)}
		m.Types[0].SubTypes = []SubType{{Final: true, Comp: CompType{Kind: CompFunc}}}
		for i := 1; i < depth; i++ {
			child := indexedRef(uint32(i-1), true)
			m.Types[i].SubTypes = []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{child, child}}}}
		}
		return m
	}

	small := graph(10)
	first, ok := small.StructuralTypeKeyChecked(9)
	if !ok {
		t.Fatal("bounded canonicalization rejected small graph")
	}
	second, ok := small.StructuralTypeKeyChecked(9)
	if !ok || second != first {
		t.Fatalf("per-call canonicalization = %#x,%v then %#x,%v", first, true, second, ok)
	}

	large := graph(20) // Former recursive expansion exceeded 1 MiB for this shared DAG.
	first, ok = large.StructuralTypeKeyChecked(19)
	if !ok || first == 0 {
		t.Fatalf("linear DAG canonicalization = %#x,%v, want success", first, ok)
	}
	if key, ok := large.StructuralTypeKeyChecked(19); !ok || key != first {
		t.Fatalf("cached large canonicalization = %#x,%v, want %#x,true", key, ok, first)
	}

	leaf := SubType{Final: true, Comp: CompType{Kind: CompFunc}}
	params := make([]ValType, 30_000)
	for i := range params {
		params[i] = indexedRef(0, true)
	}
	adversarial := &Module{Types: []RecType{
		{SubTypes: []SubType{leaf}},
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: params}}}},
	}}
	if key, ok := adversarial.StructuralTypeKeyChecked(1); ok || key != 0 {
		t.Fatalf("truly over-budget canonicalization = %#x,%v, want zero,false", key, ok)
	}
}

func TestStructuralTypeKeySeparatesDeliberateLegacyCollision(t *testing.T) {
	// These two valid signatures deliberately collide under the historical
	// 32-bit FNV-1a discriminator (0xed3a07d1). Native descriptors must use the
	// independently derived structural key instead.
	a := &CompType{Kind: CompFunc,
		Params:  []ValType{ExternRef, V128, V128, F64, FuncRef},
		Results: []ValType{F32},
	}
	b := &CompType{Kind: CompFunc,
		Params:  []ValType{ExternRef, I64, V128, F64, F32, I64},
		Results: []ValType{F64, ExternRef, I32, ExternRef},
	}
	if gotA, gotB := StructuralFuncTypeID(a), StructuralFuncTypeID(b); gotA != gotB || gotA != 0xed3a07d1 {
		t.Fatalf("deliberate legacy collision = %#x/%#x, want %#x", gotA, gotB, uint32(0xed3a07d1))
	}
	if gotA, gotB := StructuralFuncTypeKey(a), StructuralFuncTypeKey(b); gotA == gotB {
		t.Fatalf("collision-safe keys collapsed to %#x", gotA)
	}
}
