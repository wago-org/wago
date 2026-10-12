//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestStateOnlyProductionHasNoCodeImage(t *testing.T) {
	vector := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	vector = append(vector, 0x0b)
	for name, source := range map[string][]byte{
		"empty":         wasmtest.Module(),
		"scalar-types":  wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)))),
		"vector-types":  wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil)))),
		"scalar-global": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 7, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"vector-global": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, false, vector))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"vector-import": wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "g", wasm.V128, false))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"memory-data":   wasmtest.Module(wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(11, []byte{1, 0, 0x41, 0, 0x0b, 1, 42})),
	} {
		t.Run(name, func(t *testing.T) {
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, deferred := range []bool{false, true} {
				image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: deferred, Interruptible: true})
				if err != nil || image == nil {
					t.Fatal("state-only compilation", err)
				}
				if image.CodeImage != nil || len(image.Code) != 0 || len(image.Entry) != 0 || len(image.InternalEntry) != 0 {
					t.Fatal("state-only compilation allocated a code image")
				}
			}
			if image, err := Compile(source); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private scalar shape widened", err)
			}
			if err := admitSWARDecodedBudget(m); !errors.Is(err, ErrUnsupported) {
				t.Fatal("private SWAR shape widened", err)
			}
		})
	}
}

func TestStateOnlyProductionStillValidatesState(t *testing.T) {
	for _, source := range [][]byte{
		wasmtest.Module(wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("bad", 0, 0)))),
		wasmtest.Module(wasmtest.Section(8, []byte{0})),
		wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0, 0x0b})))),
	} {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal("fixture decoding", err)
		}
		staged := false
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { staged = true }})
		if err == nil || image != nil || staged {
			t.Fatal("invalid functionless state reached staging", err)
		}
	}
}

func stateOnlyMutableFuncrefImportSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
	)
}

func TestStateOnlyMutableFuncrefImportProduction(t *testing.T) {
	source := stateOnlyMutableFuncrefImportSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
	if err != nil || image == nil {
		t.Fatal("state-only mutable funcref compilation", err)
	}
	if image.CodeImage != nil || len(image.Code) != 0 || len(image.Entry) != 0 || len(image.InternalEntry) != 0 {
		t.Fatal("state-only mutable funcref allocated executable code")
	}

	for name, bad := range map[string][]byte{
		"no-export": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		),
		"extra-import": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true), wasmtest.GlobalImportEntry("env", "count", wasm.I32, true))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
		),
		"table": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
			wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
		),
	} {
		t.Run(name, func(t *testing.T) {
			decoded, err := wasm.DecodeModule(bad)
			if err != nil {
				t.Fatal(err)
			}
			image, err := CompileModuleWith(decoded, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
			if image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("unsupported mutable funcref state source admitted", err)
			}
		})
	}
}
