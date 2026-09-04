//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func arm64Memory64Module() []byte {
	memarg := func(op byte, offset uint64) []byte {
		out := []byte{op, 0x02}
		return append(out, arm64ULEB64(offset)...)
	}
	storeLoad := []byte{0x20, 0x00, 0x20, 0x01}
	storeLoad = append(storeLoad, memarg(0x36, 0)...)
	storeLoad = append(storeLoad, 0x20, 0x00)
	storeLoad = append(storeLoad, memarg(0x28, 0)...)
	storeLoad = append(storeLoad, 0x0b)
	offsetLoad := []byte{0x20, 0x00}
	offsetLoad = append(offsetLoad, memarg(0x28, ^uint64(0))...)
	offsetLoad = append(offsetLoad, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x05, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("grow", 0, 1),
			wasmtest.ExportEntry("store_load", 0, 2),
			wasmtest.ExportEntry("offset_load", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
			wasmtest.Code(storeLoad),
			wasmtest.Code(offsetLoad),
		)),
	)
}

func TestMemory64ARM64ExecutionAndCodec(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureMemory64)
	compiled, err := Compile(cfg, arm64Memory64Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := in.Invoke("size"); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
			in.Close()
			t.Fatalf("size = %v, %v", got, err)
		}
		if got, err := in.Invoke("store_load", 65532, 0x12345678); err != nil || !reflect.DeepEqual(got, []uint64{0x12345678}) {
			in.Close()
			t.Fatalf("store_load = %v, %v", got, err)
		}
		if _, err := in.Invoke("store_load", uint64(1)<<32, 1); err == nil {
			in.Close()
			t.Fatal("high memory64 address did not trap")
		}
		if _, err := in.Invoke("offset_load", 1); err == nil {
			in.Close()
			t.Fatal("wrapping memory64 static offset did not trap")
		}
		if got, err := in.Invoke("grow", 1); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
			in.Close()
			t.Fatalf("grow = %v, %v", got, err)
		}
		if got, err := in.Invoke("grow", uint64(1)<<32); err != nil || !reflect.DeepEqual(got, []uint64{^uint64(0)}) {
			in.Close()
			t.Fatalf("wide grow failure = %v, %v", got, err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
