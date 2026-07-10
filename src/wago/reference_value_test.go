package wago

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestPublicReferenceValueTypesAndNulls(t *testing.T) {
	if got := ValFuncRef.String(); got != "funcref" {
		t.Fatalf("ValFuncRef.String() = %q, want funcref", got)
	}
	if got := ValExternRef.String(); got != "externref" {
		t.Fatalf("ValExternRef.String() = %q, want externref", got)
	}

	fr := NullFuncRef()
	if !fr.IsNull() {
		t.Fatal("NullFuncRef is not null")
	}
	fv := ValueFuncRef(fr)
	if fv.Type() != ValFuncRef || fv.Bits() != 0 || !fv.FuncRef().IsNull() {
		t.Fatalf("null funcref value = type %s bits %#x ref-null %v", fv.Type(), fv.Bits(), fv.FuncRef().IsNull())
	}

	er := NullExternRef()
	if !er.IsNull() {
		t.Fatal("NullExternRef is not null")
	}
	ev := ValueExternRef(er)
	if ev.Type() != ValExternRef || ev.Bits() != 0 || !ev.ExternRef().IsNull() {
		t.Fatalf("null externref value = type %s bits %#x ref-null %v", ev.Type(), ev.Bits(), ev.ExternRef().IsNull())
	}
}

func TestPublicReferenceTokensAreOpaqueUint64Values(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{name: "FuncRef", typ: reflect.TypeOf(FuncRef{})},
		{name: "ExternRef", typ: reflect.TypeOf(ExternRef{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.typ.Kind() != reflect.Struct || tc.typ.Size() != 8 {
				t.Fatalf("%s representation = kind %s size %d, want opaque 8-byte struct", tc.name, tc.typ.Kind(), tc.typ.Size())
			}
			for i := 0; i < tc.typ.NumField(); i++ {
				field := tc.typ.Field(i)
				if field.IsExported() {
					t.Fatalf("%s field %q is exported", tc.name, field.Name)
				}
				if field.Type.Kind() != reflect.Uint64 {
					t.Fatalf("%s field %q kind = %s, want opaque uint64 token (not a Go/native pointer)", tc.name, field.Name, field.Type.Kind())
				}
			}
		})
	}
}

func TestReferenceSignatureConversionPreservesTypes(t *testing.T) {
	c := &Compiled{
		NumImports: 0,
		Funcs: []FuncSig{{
			Params:  valTypesFromWasm([]wasm.ValType{wasm.FuncRef, wasm.ExternRef}),
			Results: valTypesFromWasm([]wasm.ValType{wasm.ExternRef, wasm.FuncRef}),
		}},
		Exports: map[string]int{"refs": 0},
	}
	params, results, err := c.Signature("refs")
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	if !reflect.DeepEqual(params, []ValType{ValFuncRef, ValExternRef}) {
		t.Fatalf("params = %v, want [funcref externref]", params)
	}
	if !reflect.DeepEqual(results, []ValType{ValExternRef, ValFuncRef}) {
		t.Fatalf("results = %v, want [externref funcref]", results)
	}
}

func TestTypedCallCarriesOpaqueExternRefTokensInWideSlots(t *testing.T) {
	c := MustCompile(referenceSlotIdentityModule())
	c.Funcs[0] = FuncSig{Params: []ValType{ValExternRef}, Results: []ValType{ValExternRef}}
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	ref, err := in.NewExternRef("opaque")
	if err != nil {
		t.Fatalf("NewExternRef: %v", err)
	}
	value := ValueExternRef(ref)
	out, err := in.Call(context.Background(), "id", value)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(out) != 1 || out[0].Type() != ValExternRef || out[0].ExternRef() != ref || out[0].ExternRef().IsNull() {
		t.Fatalf("Call result = %#v, want stable opaque externref", out)
	}
	if _, err := in.Call(context.Background(), "id", ValueFuncRef(FuncRef{token: value.Bits()})); err == nil || !strings.Contains(err.Error(), "got") {
		t.Fatalf("cross-reference type mismatch error = %v", err)
	}
}

func referenceSlotIdentityModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)
}
