package wasm

import "testing"

func openStructType(fields []FieldType, supers ...TypeIdx) RecType {
	return RecType{SubTypes: []SubType{{Final: false, Supers: supers, Comp: CompType{Kind: CompStruct, Fields: fields}}}}
}

func openArrayType(elem FieldType, supers ...TypeIdx) RecType {
	return RecType{SubTypes: []SubType{{Final: false, Supers: supers, Comp: CompType{Kind: CompArray, Array: elem}}}}
}

func TestValidateGCSubtypeMetadata(t *testing.T) {
	t.Run("same-kind non-final super validates", func(t *testing.T) {
		m := &Module{Types: []RecType{
			openStructType(nil),
			{SubTypes: []SubType{{Final: true, Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}},
		}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})

	t.Run("final super is rejected", func(t *testing.T) {
		m := &Module{Types: []RecType{
			structType(nil, TypeMetadata{}),
			{SubTypes: []SubType{{Final: true, Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}},
		}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})

	t.Run("struct subtype cannot extend array super", func(t *testing.T) {
		m := &Module{Types: []RecType{
			openArrayType(field(I32, Var)),
			{SubTypes: []SubType{{Final: true, Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}},
		}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})

	t.Run("array subtype cannot extend struct super", func(t *testing.T) {
		m := &Module{Types: []RecType{
			openStructType(nil),
			{SubTypes: []SubType{{Final: true, Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompArray, Array: field(I32, Var)}}}},
		}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})

	t.Run("cyclic super chain is rejected", func(t *testing.T) {
		m := &Module{Types: []RecType{{SubTypes: []SubType{
			{Final: false, Supers: []TypeIdx{{Index: 1, Rec: true}}, Comp: CompType{Kind: CompStruct}},
			{Final: false, Supers: []TypeIdx{{Index: 0, Rec: true}}, Comp: CompType{Kind: CompStruct}},
		}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestRefTestAcceptsDefinedSiblingTypes(t *testing.T) {
	m := modWithFunc(
		[]ValType{refToType(2, true)}, nil,
		Instruction{Kind: InstrLocalGet, Index: 0},
		Instruction{Kind: InstrRefTest, ext: &instrExt{HeapType: IndexedHeap(TypeIdx{Index: 1})}},
		Instruction{Kind: InstrDrop},
	)
	m.Types = []RecType{
		openStructType(nil),
		{SubTypes: []SubType{{Final: true, Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}},
		{SubTypes: []SubType{{Final: true, Supers: []TypeIdx{{Index: 0}}, Comp: CompType{Kind: CompStruct}}}},
		ft([]ValType{refToType(2, true)}, nil),
	}
	m.FuncTypes = []TypeIdx{{Index: 3}}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ref.test between sibling defined struct types: %v", err)
	}
}

func TestRefTestHierarchyCompatibility(t *testing.T) {
	mv := &moduleValidator{m: &Module{Types: []RecType{
		openStructType(nil),
		openArrayType(field(I32, Var)),
		ft(nil, nil),
	}}}
	for _, tc := range []struct {
		name string
		a, b RefType
		ok   bool
	}{
		{name: "struct siblings", a: Ref(true, IndexedHeap(TypeIdx{Index: 0}), false), b: Ref(false, IndexedHeap(TypeIdx{Index: 0}), false), ok: true},
		{name: "struct and array data", a: Ref(true, IndexedHeap(TypeIdx{Index: 0}), false), b: Ref(false, IndexedHeap(TypeIdx{Index: 1}), false), ok: true},
		{name: "defined func and func", a: Ref(true, IndexedHeap(TypeIdx{Index: 2}), false), b: AbsRef(HeapFunc), ok: true},
		{name: "func and data", a: AbsRef(HeapFunc), b: AbsRef(HeapI31), ok: false},
		{name: "extern and data", a: AbsRef(HeapExtern), b: AbsRef(HeapAny), ok: false},
		{name: "exn and func", a: AbsRef(HeapExn), b: AbsRef(HeapFunc), ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mv.refTestCompatible(tc.a, tc.b); got != tc.ok {
				t.Fatalf("refTestCompatible(%v, %v)=%t want %t", tc.a, tc.b, got, tc.ok)
			}
		})
	}
}

func TestValidateGCRecursiveSubtypeMetadata(t *testing.T) {
	t.Run("recursive same-kind non-final super validates", func(t *testing.T) {
		m := &Module{Types: []RecType{{SubTypes: []SubType{
			{Final: false, Comp: CompType{Kind: CompStruct}},
			{Final: true, Supers: []TypeIdx{{Index: 0, Rec: true}}, Comp: CompType{Kind: CompStruct}},
		}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})

	t.Run("recursive final super is rejected", func(t *testing.T) {
		m := &Module{Types: []RecType{{SubTypes: []SubType{
			{Final: true, Comp: CompType{Kind: CompStruct}},
			{Final: true, Supers: []TypeIdx{{Index: 0, Rec: true}}, Comp: CompType{Kind: CompStruct}},
		}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})

	t.Run("recursive kind mismatch is rejected", func(t *testing.T) {
		m := &Module{Types: []RecType{{SubTypes: []SubType{
			{Final: false, Comp: CompType{Kind: CompArray, Array: field(I32, Var)}},
			{Final: true, Supers: []TypeIdx{{Index: 0, Rec: true}}, Comp: CompType{Kind: CompStruct}},
		}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})

	t.Run("recursive self-super cycle is rejected", func(t *testing.T) {
		m := &Module{Types: []RecType{{SubTypes: []SubType{
			{Final: false, Supers: []TypeIdx{{Index: 0, Rec: true}}, Comp: CompType{Kind: CompStruct}},
		}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}
