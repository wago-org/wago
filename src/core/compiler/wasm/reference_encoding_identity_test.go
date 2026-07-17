package wasm

import (
	"bytes"
	"testing"
)

func singleFuncTypeModule(param, result []byte) []byte {
	payload := []byte{0x01, 0x60, 0x01}
	payload = append(payload, param...)
	payload = append(payload, 0x01)
	payload = append(payload, result...)
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, byte(len(payload))}
	return append(module, payload...)
}

func TestReferenceEncodingFormIsNotSemanticTypeIdentity(t *testing.T) {
	bareFunc := FuncRef
	explicitFunc := RefVal(Ref(true, AbsHeap(HeapFunc), false))
	bareExtern := ExternRef
	explicitExtern := RefVal(Ref(true, AbsHeap(HeapExtern), false))

	for _, tc := range []struct {
		name     string
		bare     ValType
		explicit ValType
	}{
		{name: "func", bare: bareFunc, explicit: explicitFunc},
		{name: "extern", bare: bareExtern, explicit: explicitExtern},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.bare.Ref.Bare || tc.explicit.Ref.Bare {
				t.Fatalf("test encoding forms bare=%v explicit=%v", tc.bare.Ref.Bare, tc.explicit.Ref.Bare)
			}
			if !EqualValType(tc.bare, tc.explicit) {
				t.Fatalf("%s shorthand and explicit reference types differ semantically", tc.name)
			}
			bareType := &CompType{Kind: CompFunc, Params: []ValType{tc.bare}, Results: []ValType{tc.bare}}
			explicitType := &CompType{Kind: CompFunc, Params: []ValType{tc.explicit}, Results: []ValType{tc.explicit}}
			if !FuncTypeEqual(bareType, explicitType) {
				t.Fatalf("%s function signatures differ by encoding form", tc.name)
			}
			if got, want := StructuralFuncTypeID(bareType), StructuralFuncTypeID(explicitType); got != want {
				t.Fatalf("%s structural ids = %#x/%#x", tc.name, got, want)
			}
			if got, want := StructuralFuncTypeKey(bareType), StructuralFuncTypeKey(explicitType); got != want {
				t.Fatalf("%s structural keys = %#x/%#x", tc.name, got, want)
			}
		})
	}

	m := &Module{Types: []RecType{
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{bareFunc}}}}},
		{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{explicitFunc}}}}},
	}}
	if got := m.CanonicalTypeID(1); got != 0 {
		t.Fatalf("explicit function canonical type = %d, want shorthand type 0", got)
	}
}

func TestDecodePreservesReferenceEncodingFormWithoutChangingIdentity(t *testing.T) {
	bare, err := DecodeModule(singleFuncTypeModule([]byte{0x70}, []byte{0x6f}))
	if err != nil {
		t.Fatalf("decode shorthand module: %v", err)
	}
	explicit, err := DecodeModule(singleFuncTypeModule([]byte{0x63, 0x70}, []byte{0x63, 0x6f}))
	if err != nil {
		t.Fatalf("decode explicit module: %v", err)
	}
	bareType, ok := bare.TypeFunc(0)
	if !ok {
		t.Fatal("shorthand function type unavailable")
	}
	explicitType, ok := explicit.TypeFunc(0)
	if !ok {
		t.Fatal("explicit function type unavailable")
	}
	if !bareType.Params[0].Ref.Bare || !bareType.Results[0].Ref.Bare {
		t.Fatalf("shorthand form lost: %+v", bareType)
	}
	if explicitType.Params[0].Ref.Bare || explicitType.Results[0].Ref.Bare {
		t.Fatalf("explicit form canonicalized to shorthand: %+v", explicitType)
	}
	if !FuncTypeEqual(bareType, explicitType) {
		t.Fatal("binary shorthand and explicit function types are not equal")
	}
	if got, want := bare.StructuralTypeKey(0), explicit.StructuralTypeKey(0); got != want {
		t.Fatalf("binary shorthand/explicit keys = %#x/%#x", got, want)
	}
}

func TestStructuralTypeKeyIgnoresReferenceEncodingFormInRecursiveGroups(t *testing.T) {
	module := func(bare bool) *Module {
		ref := RefVal(Ref(true, AbsHeap(HeapFunc), false))
		if bare {
			ref = FuncRef
		}
		return &Module{Types: []RecType{{SubTypes: []SubType{
			{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{ref}}},
			{Final: true, Comp: CompType{Kind: CompStruct, Fields: []FieldType{{Storage: StorageType{Val: ref}}}}},
		}}}}
	}
	bare, explicit := module(true), module(false)
	if got, want := bare.StructuralTypeKey(0), explicit.StructuralTypeKey(0); got != want {
		t.Fatalf("recursive shorthand/explicit keys = %#x/%#x", got, want)
	}
}

func TestEncodeExprPreservesExplicitReferenceValueType(t *testing.T) {
	explicit := RefVal(Ref(true, AbsHeap(HeapFunc), false))
	if _, ok := EncodeValType(explicit); ok {
		t.Fatal("explicit reference type reported a one-byte shorthand encoding")
	}
	got, err := EncodeExpr(Expr{Instrs: []Instruction{{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{explicit}}}}})
	if err != nil {
		t.Fatalf("encode explicit typed select: %v", err)
	}
	if want := []byte{0x1c, 0x01, 0x63, 0x70, 0x0b}; !bytes.Equal(got, want) {
		t.Fatalf("explicit typed select = % x, want % x", got, want)
	}

	got, err = EncodeExpr(Expr{Instrs: []Instruction{{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{FuncRef}}}}})
	if err != nil {
		t.Fatalf("encode shorthand typed select: %v", err)
	}
	if want := []byte{0x1c, 0x01, 0x70, 0x0b}; !bytes.Equal(got, want) {
		t.Fatalf("shorthand typed select = % x, want % x", got, want)
	}
}
