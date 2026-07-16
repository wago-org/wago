package wasm

import (
	"encoding/hex"
	"testing"
)

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

func TestValidateOfficialGCTypeSubtypingValidatorGaps(t *testing.T) {
	for _, tc := range []struct {
		name    string
		hex     string
		invalid bool
	}{
		{name: "valid command 119 recursive group projection", hex: "0061736d0100000001df80808000054e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f056400006402006400006402006406004e025001066000005f000382808080000108068d8080800002640000d2000b640400d2000b0a8880808000018280808000000b"},
		{name: "valid command 143 recursive result projection", hex: "0061736d0100000001c780808000044e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e025001026000016470500106600001640603838080800002040506b18080800008640000d2000b640200d2000b640000d2010b640200d2010b640400d2000b640600d2000b640500d2010b640700d2010b0a918080800002838080800000000b838080800000000b"},
		{name: "invalid command 87 recursive group mismatch", hex: "0061736d0100000001ad80808000044e0250006000005f016400004e0250006000005f016400004e025001006000005f004e025001026000005f00038280808000010606878080800001640400d2000b0a8880808000018280808000000b", invalid: true},
		{name: "invalid command 144 recursive group mismatch", hex: "0061736d01000000019b80808000024e0250006000005001006000004e025000600000500100600000038280808000010206878080800001640000d2000b0a8880808000018280808000000b", invalid: true},
		{name: "invalid command 154 recursive group mismatch", hex: "0061736d0100000001a880808000034e0250006000005001006000004e0250006000005001006000004e025000600000500102600000038280808000010406878080800001640200d2000b0a8880808000018280808000000b", invalid: true},
		{name: "invalid command 676 array storage", hex: "0061736d01000000018c808080000250005e7f005001005e7e00", invalid: true},
		{name: "invalid command 683 struct storage", hex: "0061736d01000000018e808080000250005f017f005001005f017e00", invalid: true},
		{name: "invalid command 690 immutable array covariance", hex: "0061736d01000000018e808080000250005e6471005001005e646e00", invalid: true},
		{name: "invalid command 697 mutable array invariance", hex: "0061736d01000000018e808080000250005e646e015001005e647101", invalid: true},
		{name: "invalid command 704 array mutability removal", hex: "0061736d01000000018e808080000250005e646e015001005e646e00", invalid: true},
		{name: "invalid command 711 array mutability addition", hex: "0061736d01000000018e808080000250005e646e005001005e646e01", invalid: true},
		{name: "invalid command 718 immutable struct covariance", hex: "0061736d010000000190808080000250005f016471005001005f01646e00", invalid: true},
		{name: "invalid command 725 mutable struct invariance", hex: "0061736d010000000190808080000250005f01646e015001005f01647101", invalid: true},
		{name: "invalid command 732 struct mutability removal", hex: "0061736d010000000190808080000250005f01646e015001005f01646e00", invalid: true},
		{name: "invalid command 739 struct mutability addition", hex: "0061736d010000000190808080000250005f01646e005001005f01646e01", invalid: true},
		{name: "invalid command 746 function parameter contravariance", hex: "0061736d01000000018d8080800002500060000050010060017f00", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			m, err := DecodeModule(data)
			if err != nil {
				t.Fatalf("DecodeModule: %v", err)
			}
			for _, path := range []struct {
				name string
				fn   func() error
			}{
				{name: "AST", fn: func() error { return ValidateModule(m) }},
				{name: "byte-backed", fn: func() error { return ValidateByteBackedModule(data) }},
			} {
				t.Run(path.name, func(t *testing.T) {
					err := path.fn()
					if !tc.invalid {
						if err != nil {
							t.Fatalf("validation: %v", err)
						}
						return
					}
					expectValidationCode(t, err, ErrTypeMismatch)
				})
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
