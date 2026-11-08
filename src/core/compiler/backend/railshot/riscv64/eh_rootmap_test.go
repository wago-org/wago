//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBuildExceptionRootMapsTryTableExplicitReferenceBlockType(t *testing.T) {
	const typeIndex = 6
	body := []byte{
		0x1f, 0x64, typeIndex, 0x00, // try_table (result (ref 6)), no catches
		0xfb, 0x00, typeIndex, // struct.new 6
		0x0b, 0x0b, // end try_table, function
	}
	types := make([][]byte, typeIndex+2)
	for i := 0; i <= typeIndex; i++ {
		types[i] = []byte{0x5f, 0x00}
	}
	types[typeIndex+1] = []byte{0x60, 0x00, 0x01, 0x64, typeIndex}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{typeIndex + 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		build func(*wasm.Module) ([]nativeabi.FunctionRootMap, error)
	}{
		{"public", BuildExceptionRootMaps},
		{"unowned", func(m *wasm.Module) ([]nativeabi.FunctionRootMap, error) {
			return BuildExceptionRootMapsOwned(m, false)
		}},
		{"owned", func(m *wasm.Module) ([]nativeabi.FunctionRootMap, error) { return BuildExceptionRootMapsOwned(m, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			maps, err := tc.build(m)
			if err != nil {
				t.Fatal(err)
			}
			if len(maps) != 0 {
				t.Fatalf("tag-free fixture has root maps: %#v", maps)
			}
		})
	}
}
