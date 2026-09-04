//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func arm64MultiMemoryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x01},
			[]byte{0x01, 0x01, 0x03},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size1", 0, 0),
			wasmtest.ExportEntry("store1", 0, 1),
			wasmtest.ExportEntry("load1", 0, 2),
			wasmtest.ExportEntry("grow1", 0, 3),
			wasmtest.ExportEntry("fill1_const", 0, 4),
			wasmtest.ExportEntry("copy10_const", 0, 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x3f, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x36, 0x42, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x42, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x01, 0x0b}),
			wasmtest.Code([]byte{
				0x41, 0x01, 0x41, 0x02, 0x41, 0x02, // dst=1, byte=2, len=2
				0xfc, 0x0b, 0x01, // memory.fill 1
				0x41, 0x01, 0x28, 0x42, 0x01, 0x00, 0x0b, // i32.load memory 1
			}),
			wasmtest.Code([]byte{
				0x41, 0x03, 0x41, 0x01, 0x41, 0x02, // dst=3, src=1, len=2
				0xfc, 0x0a, 0x00, 0x01, // memory.copy 0 1
				0x41, 0x03, 0x28, 0x02, 0x00, 0x0b, // i32.load memory 0
			}),
		)),
	)
}

func TestMultiMemoryARM64ExecutionAndCodec(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureMultiMemory)
	compiled, err := Compile(cfg, arm64MultiMemoryModule())
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
		if got, err := in.Invoke("size1"); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
			in.Close()
			t.Fatalf("size1 = %v, %v", got, err)
		}
		if _, err := in.Invoke("store1", 65532, 0x12345678); err != nil {
			in.Close()
			t.Fatal(err)
		}
		if got, err := in.Invoke("load1", 65532); err != nil || !reflect.DeepEqual(got, []uint64{0x12345678}) {
			in.Close()
			t.Fatalf("load1 = %v, %v", got, err)
		}
		if got, err := in.Invoke("grow1", 1); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
			in.Close()
			t.Fatalf("grow1 = %v, %v", got, err)
		}
		if got, err := in.Invoke("size1"); err != nil || !reflect.DeepEqual(got, []uint64{2}) {
			in.Close()
			t.Fatalf("size1 after grow = %v, %v", got, err)
		}
		if got, err := in.Invoke("fill1_const"); err != nil || !reflect.DeepEqual(got, []uint64{0x0202}) {
			in.Close()
			t.Fatalf("fill1_const = %v, %v", got, err)
		}
		if got, err := in.Invoke("copy10_const"); err != nil || !reflect.DeepEqual(got, []uint64{0x0202}) {
			in.Close()
			t.Fatalf("copy10_const = %v, %v", got, err)
		}
		if _, err := in.Invoke("load1", 2*65536); err == nil {
			in.Close()
			t.Fatal("indexed memory out-of-bounds load did not trap")
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
