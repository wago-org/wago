package wasm

import (
	"errors"
	"testing"
)

func expectValidationCode(t *testing.T, err error, code ValidationErrorCode) {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != code {
		t.Fatalf("validation error = %v, want %v", err, code)
	}
}

func TestValidateIndexedFunctionReferenceEquivalence(t *testing.T) {
	t.Run("duplicate function types", func(t *testing.T) {
		m := &Module{
			Types: []RecType{
				ft([]ValType{F32, F32}, []ValType{F32}),
				ft([]ValType{F32, F32}, []ValType{F32}),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))}, nil),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1}), false))}, nil),
			},
			FuncTypes: []TypeIdx{{Index: 2}, {Index: 3}},
			Code: []Func{
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 1}}}},
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 0}}}},
			},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule duplicate types: %v", err)
		}
	})

	t.Run("separate recursive groups", func(t *testing.T) {
		self := func() RecType {
			return RecType{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{
				I32, RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0, Rec: true}), false)),
			}}}}}
		}
		m := &Module{
			Types: []RecType{
				self(),
				self(),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))}, nil),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1}), false))}, nil),
			},
			FuncTypes: []TypeIdx{{Index: 2}, {Index: 3}},
			Code: []Func{
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 1}}}},
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 0}}}},
			},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule recursive types: %v", err)
		}
	})

	t.Run("different types remain distinct", func(t *testing.T) {
		m := &Module{
			Types: []RecType{
				ft([]ValType{I32}, nil),
				ft([]ValType{F32}, nil),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))}, nil),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1}), false))}, nil),
			},
			FuncTypes: []TypeIdx{{Index: 2}, {Index: 3}},
			Code: []Func{
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 1}}}},
				{Body: Expr{}},
			},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})

	t.Run("recursive group boundary remains nominal", func(t *testing.T) {
		m := &Module{Types: []RecType{
			{SubTypes: []SubType{
				{Final: true, Comp: CompType{Kind: CompFunc}},
				{Final: true, Comp: CompType{Kind: CompFunc}},
			}},
			ft(nil, nil),
		}, FuncTypes: []TypeIdx{{Index: 2}}, Code: []Func{{Body: Expr{}}}, Globals: []Global{{
			Type: GlobalType{Type: RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))},
			Init: Expr{Instrs: []Instruction{{Kind: InstrRefFunc, Index: 0}}},
		}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestValidateRecursiveTypeScopeASTAndByteBacked(t *testing.T) {
	forward := &Module{Types: []RecType{
		ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1}), false))}, nil),
		ft(nil, nil),
	}}
	expectValidateErr(t, forward, ErrUnknownType)

	// Exact pinned Release 3 encodings for the forward-reference invalid modules.
	// Both the structured and direct byte-backed validators must reject before
	// any frontend/product feature gate can classify them as unsupported.
	for _, tc := range []struct {
		name string
		data []byte
		want ValidationErrorCode
	}{
		{"type-equivalence line 46", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x8b, 0x80, 0x80, 0x80, 0x00, 0x02, 0x60, 0x01, 0x64, 0x01, 0x00, 0x60, 0x01, 0x64, 0x00, 0x00}, ErrUnknownType},
		{"type-rec line 9", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x89, 0x80, 0x80, 0x80, 0x00, 0x02, 0x60, 0x01, 0x64, 0x01, 0x00, 0x60, 0x00, 0x00}, ErrUnknownType},
		{"type-rec line 16", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x89, 0x80, 0x80, 0x80, 0x00, 0x02, 0x60, 0x01, 0x64, 0x01, 0x00, 0x60, 0x00, 0x00}, ErrUnknownType},
		{"type-rec line 37", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x8c, 0x80, 0x80, 0x80, 0x00, 0x02, 0x4e, 0x02, 0x60, 0x00, 0x00, 0x60, 0x00, 0x00, 0x60, 0x00, 0x00, 0x03, 0x82, 0x80, 0x80, 0x80, 0x00, 0x01, 0x02, 0x06, 0x87, 0x80, 0x80, 0x80, 0x00, 0x01, 0x64, 0x00, 0x00, 0xd2, 0x00, 0x0b, 0x0a, 0x88, 0x80, 0x80, 0x80, 0x00, 0x01, 0x82, 0x80, 0x80, 0x80, 0x00, 0x00, 0x0b}, ErrTypeMismatch},
		{"type-rec line 46", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x8c, 0x80, 0x80, 0x80, 0x00, 0x02, 0x4e, 0x02, 0x60, 0x00, 0x00, 0x60, 0x00, 0x00, 0x60, 0x00, 0x00, 0x03, 0x82, 0x80, 0x80, 0x80, 0x00, 0x01, 0x02, 0x06, 0x87, 0x80, 0x80, 0x80, 0x00, 0x01, 0x64, 0x01, 0x00, 0xd2, 0x00, 0x0b, 0x0a, 0x88, 0x80, 0x80, 0x80, 0x00, 0x01, 0x82, 0x80, 0x80, 0x80, 0x00, 0x00, 0x0b}, ErrTypeMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := DecodeModule(tc.data)
			if err != nil {
				t.Fatalf("DecodeModule: %v", err)
			}
			expectValidationCode(t, ValidateModule(m), tc.want)
			expectValidationCode(t, ValidateByteBackedModule(tc.data), tc.want)
		})
	}
}
