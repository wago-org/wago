//go:build linux && (amd64 || arm64)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

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
