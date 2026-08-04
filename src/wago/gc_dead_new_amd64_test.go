//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcDeadNewModule(types [][]byte, funcType uint32, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(funcType))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCDeadConstructorTreeExecutesWithoutAllocationHelpers(t *testing.T) {
	arrayType := []byte{0x5e, 0x7f, 0x01}
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec(
		[]byte{0x63, 0x00, 0x00},
		[]byte{0x7f, 0x00},
	)...)
	data := gcDeadNewModule([][]byte{
		arrayType,
		structType,
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}, 2, []byte{
		0x41, 0x2a,
		0x41, 0x01, 0x41, 0x02,
		0xfb, 0x08, 0x00, 0x02,
		0x41, 0x16,
		0xfb, 0x00, 0x01,
		0x1a,
		0x0b,
	})
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{
		GC: GCConfig{Profile: GCProfileThroughput, ThroughputHeapBytes: 64 * 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 42 {
		t.Fatalf("run = %v, want [42]", got)
	}
}

func TestGCDeadConstructorPreservesInitializerTrap(t *testing.T) {
	structType := []byte{0x5f, 0x01, 0x7f, 0x00}
	data := gcDeadNewModule([][]byte{
		structType,
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}, 1, []byte{
		0x41, 0x01,
		0x41, 0x00,
		0x6d, // i32.div_s: must trap before the dead allocation/drop
		0xfb, 0x00, 0x00,
		0x1a,
		0x41, 0x07,
		0x0b,
	})
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err == nil {
		t.Fatalf("run = %v, want divide-by-zero trap", got)
	}
}
