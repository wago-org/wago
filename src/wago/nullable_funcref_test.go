package wago

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestNullableFuncrefParamsResultsLocalsAndInstructions(t *testing.T) {
	c, err := Compile(nil, nullableFuncrefModule())
	if err != nil {
		t.Fatalf("Compile nullable funcref module: %v", err)
	}
	defer c.Close()

	params, results, err := c.Signature("id")
	if err != nil {
		t.Fatalf("Signature id: %v", err)
	}
	if !reflect.DeepEqual(params, []ValType{ValFuncRef}) || !reflect.DeepEqual(results, []ValType{ValFuncRef}) {
		t.Fatalf("id signature = %v -> %v, want [funcref] -> [funcref]", params, results)
	}

	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	out, err := in.Call(context.Background(), "id", ValueFuncRef(NullFuncRef()))
	if err != nil {
		t.Fatalf("Call id(null): %v", err)
	}
	if len(out) != 1 || out[0].Type() != ValFuncRef || !out[0].FuncRef().IsNull() {
		t.Fatalf("Call id(null) = %v, want one null funcref", out)
	}

	for _, name := range []string{"local_zero", "call_id", "block_ref"} {
		t.Run(name, func(t *testing.T) {
			out, err := in.Call(context.Background(), name)
			if err != nil {
				t.Fatalf("Call %s: %v", name, err)
			}
			if len(out) != 1 || out[0].Type() != ValFuncRef || !out[0].FuncRef().IsNull() {
				t.Fatalf("Call %s = %v, want one null funcref", name, out)
			}
		})
	}

	isNull, err := in.Invoke("is_null")
	if err != nil {
		t.Fatalf("Invoke is_null: %v", err)
	}
	if !reflect.DeepEqual(isNull, []uint64{I32(1)}) {
		t.Fatalf("is_null = %v, want [1]", isNull)
	}
}

func TestNullableFuncrefTypesRespectReferenceFeatureGate(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	_, err := Compile(cfg, nullableFuncrefModule())
	if err == nil || !strings.Contains(err.Error(), "reference-types disabled") || !strings.Contains(err.Error(), "funcref") {
		t.Fatalf("Compile with reference types disabled error = %v, want funcref reference-types gate", err)
	}
}

func nullableFuncrefModule() []byte {
	funcref := wasm.FuncRef
	returnFuncref := wasmtest.FuncType(nil, []wasm.ValType{funcref})
	returnI32 := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	returnFuncrefIndex := byte(1)

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{funcref}, []wasm.ValType{funcref}),
			returnFuncref,
			returnI32,
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0),
			wasmtest.ULEB(1),
			wasmtest.ULEB(2),
			wasmtest.ULEB(1),
			wasmtest.ULEB(1),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("id", 0, 0),
			wasmtest.ExportEntry("local_zero", 0, 1),
			wasmtest.ExportEntry("is_null", 0, 2),
			wasmtest.ExportEntry("call_id", 0, 3),
			wasmtest.ExportEntry("block_ref", 0, 4),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			codeWithLocalRun(1, wasm.MustEncodeValType(funcref), []byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x02, returnFuncrefIndex, 0xd0, 0x70, 0x0b, 0x0b}),
		)),
	)
}

func codeWithLocalRun(count uint32, typ byte, body []byte) []byte {
	fn := wasmtest.Vec(append(wasmtest.ULEB(count), typ))
	fn = append(fn, body...)
	return append(wasmtest.ULEB(uint32(len(fn))), fn...)
}
