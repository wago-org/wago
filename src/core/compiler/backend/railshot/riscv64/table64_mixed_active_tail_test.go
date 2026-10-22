//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func mixedTable64ActiveTailSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 4, 4)
	importedActive := []byte{0x04, 0x42, 1, 0x0b, 0x02, 0xd2, 0x00, 0x0b, 0xd2, 0x01, 0x0b}
	ownedActive := []byte{0x06, 0x01, 0x42, 1, 0x0b, 0x70, 0x02, 0xd2, 0x00, 0x0b, 0xd2, 0x01, 0x0b}
	tail := func(table byte) []byte { return []byte{0x20, 0, 0x13, 0, table, 0x0b} }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 4, 4})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("tail0", byte(wasm.ExternFunc), 2), wasmtest.ExportEntry("tail1", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0), wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(importedActive, ownedActive)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}), wasmtest.Code([]byte{0x41, 8, 0x0b}),
			wasmtest.Code(tail(0)), wasmtest.Code(tail(1)),
		)),
	)
}

func TestProductionMixedTable64ActiveTailPlans(t *testing.T) {
	opts := CompileOptions{NativeTailCalls: true, InvocationContext: true, Interruptible: true, ForeignTableCalls: true, DeferCodeMapping: true}
	a, err := CompileModuleWith(localTailDecode(t, mixedTable64ActiveTailSource()), opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CompileModuleWith(localTailDecode(t, mixedTable64ActiveTailSource()), opts)
	if err != nil {
		t.Fatal(err)
	}
	if a.CodeImage != nil || a.NativeTail == nil || len(a.NativeTail.Plans) != 2 || !bytes.Equal(a.Code, b.Code) || !slices.Equal(a.NativeTail.Plans, b.NativeTail.Plans) {
		t.Fatal("mixed table64 active tails are not address-free and deterministic")
	}
	for i, p := range a.NativeTail.Plans {
		if p.Kind != 1 || p.TypeIndex != 0 || p.FunctionIndex != uint32(i+2) || p.ImportIndex != 0 || p.SourcePC != 3 || p.FrameBytes != 64 || p.SourceBase != 56 || p.ReferenceBase != 56 || p.SourceStride != 8 {
			t.Fatal("mixed table64 active tail plan", i, p)
		}
	}
}
