//go:build linux && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestImportOnlyRV64ProductionImageAndPrivateGates(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.V128} {
		t.Run(typ.String(), func(t *testing.T) {
			entry := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))), wasmtest.Section(2, wasmtest.Vec(entry)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("relay", 0, 0))))
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := AdmitProductionSource(data); err != nil {
				t.Fatal(err)
			}
			image, err := CompileModuleWith(m, CompileOptions{ImportBindings: hostImportBindings(1), Interruptible: true, DeferCodeMapping: true})
			if err != nil || image == nil {
				t.Fatal("import-only public staging", err)
			}
			if len(image.Code) != 0 || len(image.Entry) != 0 || len(image.InternalEntry) != 0 || image.CodeImage != nil {
				t.Fatal("import-only staging invented defined code")
			}
			for _, compile := range []func([]byte) (*Compiled, error){Compile, func(data []byte) (*Compiled, error) {
				return CompileWithOptions(data, CompileOptions{production: true, ImportBindings: hostImportBindings(1)})
			}} {
				if image, err := compile(data); image != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("private byte policy widened", err)
				}
			}
			if err := admitSWARDecodedBudget(m); !errors.Is(err, ErrUnsupported) {
				t.Fatal("private SWAR shape widened", err)
			}
		})
	}
	for _, typ := range []wasm.ValType{wasm.I32, wasm.V128} {
		data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, nil))))
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		pressure := 0
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
		if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
			t.Fatal("functionless module reached staging", err, pressure)
		}
	}
}
