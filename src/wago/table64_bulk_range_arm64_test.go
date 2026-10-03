//go:build (linux || darwin) && arm64

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func arm64Table64BulkRangeModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x04, 0x01}, // table64 funcref, min 1
			[]byte{0x6f, 0x04, 0x01}, // table64 externref, min 1
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fill_func", 0, 0),
			wasmtest.ExportEntry("fill_extern", 0, 1),
			wasmtest.ExportEntry("copy", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x6f, 0x20, 0x01, 0xfc, 0x11, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}),
		)),
	)
}

func TestARM64Table64BulkRangeAdditionTrapsOnCarry(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureTable64), arm64Table64BulkRangeModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	cases := []struct {
		name string
		args []uint64
	}{
		{"fill_func", []uint64{^uint64(0), 1}},
		{"fill_extern", []uint64{^uint64(0), 1}},
		{"copy_dst", []uint64{^uint64(0), 0, 1}},
		{"copy_src", []uint64{0, ^uint64(0), 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := tc.name
			if name == "copy_dst" || name == "copy_src" {
				name = "copy"
			}
			_, err := instance.Invoke(name, tc.args...)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapTableOutOfBounds {
				t.Fatalf("Invoke = %v; want %v", err, TrapTableOutOfBounds)
			}
		})
	}
}
