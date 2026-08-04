//go:build (linux || darwin) && arm64

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func arm64ULEB64(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func arm64Table64Module() []byte {
	table := []byte{0x70, 0x05}
	table = append(table, arm64ULEB64(2)...)
	table = append(table, arm64ULEB64(4)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("clear", 0, 1),
			wasmtest.ExportEntry("is_null", 0, 2),
			wasmtest.ExportEntry("grow", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
		)),
	)
}

func TestTable64ARM64ExecutionAndCodec(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureTable64)
	compiled, err := Compile(cfg, arm64Table64Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, candidate := range []*Compiled{compiled, roundTripCompiled(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := in.Invoke("size"); err != nil || !reflect.DeepEqual(got, []uint64{2}) {
			in.Close()
			t.Fatalf("size = %v, %v", got, err)
		}
		if got, err := in.Invoke("grow", 2); err != nil || !reflect.DeepEqual(got, []uint64{2}) {
			in.Close()
			t.Fatalf("grow = %v, %v", got, err)
		}
		if _, err := in.Invoke("clear", 3); err != nil {
			in.Close()
			t.Fatal(err)
		}
		if got, err := in.Invoke("is_null", 3); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
			in.Close()
			t.Fatalf("is_null = %v, %v", got, err)
		}
		if _, err := in.Invoke("is_null", uint64(1)<<32); err == nil {
			in.Close()
			t.Fatal("high table64 index did not trap")
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
