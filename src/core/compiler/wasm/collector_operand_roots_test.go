package wasm

import "testing"

func TestFunctionElementSubtype(t *testing.T) {
	m := &Module{Types: []RecType{
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Results: []ValType{I32}}}}},
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Results: []ValType{I64}}}}},
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompStruct}}}},
	}, FuncTypes: []TypeIdx{{Index: 0}, {Index: 1}}}
	nullable := Ref(true, IndexedHeap(TypeIdx{Index: 0}), false)
	for _, test := range []struct {
		name     string
		body     []byte
		required RefType
		want     bool
	}{
		{"function", []byte{0xd2, 0, 0x0b}, nullable, true},
		{"typed null", []byte{0xd0, 0, 0x0b}, nullable, true},
		{"bottom null", []byte{0xd0, byte(HeapNoFunc), 0x0b}, nullable, true},
		{"wrong function", []byte{0xd2, 1, 0x0b}, nullable, false},
		{"unknown function", []byte{0xd2, 2, 0x0b}, nullable, false},
		{"struct null", []byte{0xd0, 2, 0x0b}, nullable, false},
		{"wrong nullable", []byte{0xd0, 0, 0x0b}, Ref(false, IndexedHeap(TypeIdx{Index: 0}), false), false},
		{"global", []byte{0x23, 0, 0x0b}, nullable, false},
		{"trailing instruction", []byte{0xd2, 0, 0x1a, 0x0b}, nullable, false},
		{"trailing bytes", []byte{0xd2, 0, 0x0b, 0x0b}, nullable, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, got := FunctionElementSubtype(m, Expr{BodyBytes: test.body}, test.required); got != test.want {
				t.Fatalf("element accepted=%v, want %v", got, test.want)
			}
		})
	}
}

func TestExternStorageMutation(t *testing.T) {
	extern := StorageVal(ExternRef)
	numeric := StorageVal(I32)
	m := &Module{Types: []RecType{{SubTypes: []SubType{
		{Comp: CompType{Kind: CompStruct, Fields: []FieldType{NewFieldType(extern, Var), NewFieldType(extern, Const), NewFieldType(numeric, Var)}}},
		{Comp: CompType{Kind: CompArray, Array: NewFieldType(extern, Var)}},
		{Comp: CompType{Kind: CompArray, Array: NewFieldType(numeric, Var)}},
	}}}}
	for _, test := range []struct {
		kind       InstrKind
		typ, field uint32
		want       bool
	}{
		{InstrStructSet, 0, 0, true}, {InstrStructSet, 0, 1, false}, {InstrStructSet, 0, 2, false}, {InstrStructGet, 0, 0, false},
		{InstrArraySet, 1, 0, true}, {InstrArrayFill, 1, 0, true}, {InstrArrayInitElem, 1, 0, true}, {InstrArraySet, 2, 0, false},
		{InstrArraySet, 9, 0, false}, {InstrStructSet, 0, 9, false},
	} {
		if got := ExternStorageMutation(m, test.kind, test.typ, test.field); got != test.want {
			t.Errorf("kind=%v type=%d field=%d: %t want %t", test.kind, test.typ, test.field, got, test.want)
		}
		if FunctionStorageMutation(m, test.kind, test.typ, test.field) {
			t.Error("extern field classified as function")
		}
	}
}

func TestExternPersistentMutation(t *testing.T) {
	m := &Module{
		Globals: []Global{{Type: GlobalType{Type: ExternRef, Mutable: true}}, {Type: GlobalType{Type: ExternRef}}, {Type: GlobalType{Type: I32, Mutable: true}}},
		Tables:  []Table{{Type: TableType{Ref: ExternRef.Ref()}}, {Type: TableType{Ref: FuncRef.Ref()}}},
	}
	for _, test := range []struct {
		kind          InstrKind
		index, index2 uint32
		want          bool
	}{
		{InstrGlobalSet, 0, 0, true}, {InstrGlobalSet, 1, 0, false}, {InstrGlobalSet, 2, 0, false}, {InstrGlobalGet, 0, 0, false},
		{InstrTableSet, 0, 0, true}, {InstrTableFill, 0, 0, true}, {InstrTableGrow, 0, 0, true}, {InstrTableCopy, 0, 1, true},
		{InstrTableInit, 99, 0, true}, {InstrTableInit, 0, 1, false}, {InstrTableSet, 1, 0, false}, {InstrTableGet, 0, 0, false}, {InstrElemDrop, 0, 0, false}, {InstrTableSet, 9, 0, false},
	} {
		if got := ExternPersistentMutation(m, test.kind, test.index, test.index2); got != test.want {
			t.Errorf("kind=%v index=%d/%d: %t want %t", test.kind, test.index, test.index2, got, test.want)
		}
	}
}
