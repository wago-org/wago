//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func importedTableOptionFixture(vector, hasMax bool, minimum uint32) []byte {
	value := wasm.I32
	if vector {
		value = wasm.V128
	}
	table := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70)
	if hasMax {
		table = append(table, 1)
	} else {
		table = append(table, 0)
	}
	table = append(table, wasmtest.ULEB(minimum)...)
	if hasMax {
		table = append(table, wasmtest.ULEB(minimum+1)...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{value}, []wasm.ValType{value}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(table)), wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 15, 0, 0x0b}))))
}

func TestProductionImportedTableRequiresQualifiedOptions(t *testing.T) {
	for _, vector := range []bool{false, true} {
		source := importedTableOptionFixture(vector, true, 1)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		pressure := 0
		for _, opts := range []CompileOptions{{}, {Interruptible: true}, {ForeignTableCalls: true}} {
			opts.DeferCodeMapping = true
			opts.MemoryPressure = func() { pressure++ }
			image, err := CompileModuleWith(m, opts)
			if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("unqualified imported table staged", vector, err, pressure)
			}
		}
		if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true}); err != nil || image == nil || len(image.Code) == 0 {
			t.Fatal("qualified imported table", vector, err)
		}
		if table, err := admitOwnedTable(m, true); table || !errors.Is(err, ErrUnsupported) {
			t.Fatal("private owner admission widened", err)
		}
	}
}

func TestProductionImportedTableNoMaxGrowthKeepsOwnedReserveGate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m, err := wasm.DecodeModule(importedTableOptionFixture(vector, false, 600))
		if err != nil {
			t.Fatal(err)
		}
		if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true}); err != nil || image == nil {
			t.Fatal("borrowed bounded storage reserve", vector, err)
		}
		m.Imports = nil
		m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 600}}}}
		if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true}); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("owned no-max reserve gate widened", vector, err)
		}
	}
}

func TestProductionImportedTableDecodedBoundsPrecedeTraversalAndStaging(t *testing.T) {
	m, err := wasm.DecodeModule(importedTableOptionFixture(false, true, 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"count", "capacity", "second", "table64", "externref"} {
		copy := *m
		copy.Imports = append([]wasm.Import(nil), m.Imports...)
		table := copy.Imports[0].Type.TableType()
		switch kind {
		case "count":
			copy.Imports = make([]wasm.Import, MaxProductionImports+1)
		case "capacity":
			table.Limits.Max = 1025
			copy.Imports[0].Type = wasm.NewTableExternType(table)
		case "second":
			copy.Tables = []wasm.Table{{Type: table}}
		case "table64":
			table.Limits.Addr64 = true
			copy.Imports[0].Type = wasm.NewTableExternType(table)
		case "externref":
			table.Ref = wasm.ExternRef.Ref()
			copy.Imports[0].Type = wasm.NewTableExternType(table)
		}
		pressure := 0
		image, err := CompileModuleWith(&copy, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, MemoryPressure: func() { pressure++ }})
		if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
			t.Fatal("invalid decoded import staged", kind, err)
		}
		if kind == "count" && !strings.Contains(err.Error(), "count bound") {
			t.Fatal("decoded count gate lost", err)
		}
	}
}
