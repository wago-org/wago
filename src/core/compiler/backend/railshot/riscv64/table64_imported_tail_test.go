//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func importedTable64TailSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 1, 4)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("tail", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x13, 0, 0, 0x0b}))),
	)
}

func TestProductionImportedTable64TailPlan(t *testing.T) {
	opts := CompileOptions{NativeTailCalls: true, InvocationContext: true, Interruptible: true, ForeignTableCalls: true, DeferCodeMapping: true}
	a, err := CompileModuleWith(localTailDecode(t, importedTable64TailSource()), opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CompileModuleWith(localTailDecode(t, importedTable64TailSource()), opts)
	if err != nil {
		t.Fatal(err)
	}
	if a.CodeImage != nil || a.NativeTail == nil || len(a.NativeTail.Plans) != 1 || !bytes.Equal(a.Code, b.Code) || !slices.Equal(a.NativeTail.Plans, b.NativeTail.Plans) {
		t.Fatal("imported table64 tail is not address-free and deterministic")
	}
	p := a.NativeTail.Plans[0]
	if p.Kind != 1 || p.TypeIndex != 0 || p.FunctionIndex != 0 || p.ImportIndex != 0 || p.SourcePC != 3 || p.FrameBytes != 64 || p.SourceBase != 56 || p.ReferenceBase != 56 || p.SourceStride != 8 {
		t.Fatal("imported table64 tail plan", p)
	}
}
