//go:build linux && (amd64 || arm64)

package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestStubPageSnapshotRestoresBytesAndGlobalsExactly(t *testing.T) {
	tableTestForceExplicitBounds(t)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(5, []byte{0x01, 0x00, 0x02}),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x10, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x29, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("__host_reset_cursor", 0, 0),
			wasmtest.ExportEntry("memory", 2, 0),
			wasmtest.ExportEntry("cursor", 3, 0),
			wasmtest.ExportEntry("value", 3, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x08, 0x24, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer instance.Close()

	snapshot, binding, err := CaptureStubPageSnapshot(instance)
	if err != nil {
		t.Fatalf("CaptureStubPageSnapshot: %v", err)
	}
	defer snapshot.Close()
	defer binding.Close()
	baseline := append([]byte(nil), instance.Memory().Bytes()...)

	memory := instance.Memory().Bytes()
	memory[17] = 0xaa
	memory[len(memory)/2+31] = 0xbb
	memory[len(memory)-1] = 0xcc
	if err = instance.SetGlobal("cursor", I32(77)); err != nil {
		t.Fatalf("SetGlobal cursor: %v", err)
	}
	if err = instance.SetGlobal("value", I32(99)); err != nil {
		t.Fatalf("SetGlobal value: %v", err)
	}

	if err = binding.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !bytes.Equal(instance.Memory().Bytes(), baseline) {
		t.Fatal("Reset did not restore linear memory byte-for-byte")
	}
	if got, globalErr := instance.Global("cursor"); globalErr != nil || AsI32(got) != 16 {
		t.Fatalf("cursor after reset = %v, %v; want 16", got, globalErr)
	}
	if got, globalErr := instance.Global("value"); globalErr != nil || AsI32(got) != 41 {
		t.Fatalf("value after reset = %v, %v; want 41", got, globalErr)
	}
}

func TestPageSnapshotRestoresPassiveElementDropState(t *testing.T) {
	tableTestForceExplicitBounds(t)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		tableTestFuncSection(0, 1, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(5, []byte{0x01, 0x00, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 1),
			wasmtest.ExportEntry("drop", 0, 2),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(7))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestI32Const(1), tableTestBulk(12, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestBulk(13, 0))),
		)),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer instance.Close()

	snapshot, binding, err := CapturePageSnapshot(instance)
	if err != nil {
		t.Fatalf("CapturePageSnapshot: %v", err)
	}
	defer snapshot.Close()
	defer binding.Close()

	if _, err = instance.Invoke("drop"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err = instance.Invoke("init"); err == nil {
		t.Fatal("table.init after elem.drop succeeded before reset")
	}
	if err = binding.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, err = instance.Invoke("init"); err != nil {
		t.Fatalf("table.init after reset: %v", err)
	}
}

func TestPageSnapshotRejectsMutableImportedState(t *testing.T) {
	tests := []struct {
		name    string
		compile func(t *testing.T) (*Compiled, Imports)
	}{
		{
			name: "global",
			compile: func(t *testing.T) (*Compiled, Imports) {
				t.Helper()
				globalImport := append(wasmtest.Name("env"), wasmtest.Name("g")...)
				globalImport = append(globalImport, 0x03, byte(wasm.NumI32), 0x01)
				mod := wasmtest.Module(
					wasmtest.Section(2, wasmtest.Vec(globalImport)),
					wasmtest.Section(5, []byte{0x01, 0x00, 0x01}),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
				)
				compiled, err := Compile(nil, mod)
				if err != nil {
					t.Fatalf("Compile: %v", err)
				}
				return compiled, Imports{"env.g": NewGlobalI32(0, true)}
			},
		},
		{
			name: "table",
			compile: func(t *testing.T) (*Compiled, Imports) {
				t.Helper()
				mod := wasmtest.Module(
					wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
					wasmtest.Section(5, []byte{0x01, 0x00, 0x01}),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
				)
				compiled, err := Compile(nil, mod)
				if err != nil {
					t.Fatalf("Compile: %v", err)
				}
				table, err := NewTable(1, 1)
				if err != nil {
					t.Fatalf("NewTable: %v", err)
				}
				t.Cleanup(func() { _ = table.Close() })
				return compiled, Imports{"env.table": table}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tableTestForceExplicitBounds(t)
			compiled, imports := test.compile(t)
			instance, err := Instantiate(compiled, imports)
			if err != nil {
				t.Fatalf("Instantiate: %v", err)
			}
			defer instance.Close()
			if _, _, err = CapturePageSnapshot(instance); err == nil {
				t.Fatal("CapturePageSnapshot accepted mutable imported state")
			}
		})
	}
}
