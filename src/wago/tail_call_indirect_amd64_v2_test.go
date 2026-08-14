//go:build (linux || darwin) && amd64 && !tinygo

package wago

import (
	"math"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func amd64MixedResultReturnCallIndirectModule() []byte {
	sig := wasmtest.FuncType(
		[]wasm.ValType{wasm.F64, wasm.I64},
		[]wasm.ValType{wasm.I32, wasm.F32},
	)
	table := []byte{byte(wasm.HeapFunc), 0x00, 0x01}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x01}
	swizzle := []byte{0x20, 0x01, 0xa7, 0x20, 0x00, 0xb6, 0x0b}
	forward := []byte{0x20, 0x00, 0x20, 0x01, 0x12, 0x00, 0x0b}
	caller := []byte{0x20, 0x00, 0x20, 0x01, 0x41, 0x00, 0x13, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(swizzle), wasmtest.Code(forward), wasmtest.Code(caller))),
	)
}

func TestAMD64ReturnCallIndirectPreservesMixedRegisterResults(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), amd64MixedResultReturnCallIndirectModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	got, callErr := in.Invoke("run", math.Float64bits(7.5), 123)
	want := []uint64{123, uint64(math.Float32bits(7.5))}
	if callErr != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed-result return_call_indirect = %#x, %v; want %#x, nil", got, callErr, want)
	}
}
