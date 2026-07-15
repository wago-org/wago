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
