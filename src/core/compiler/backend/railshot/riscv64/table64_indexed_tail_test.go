//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func indexedTable64TailSource() []byte {
	declaredTarget := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	seed := func(table byte) []byte { return []byte{0x20, 0, 0xd2, 0, 0x26, table, 0x41, 1, 0x0b} }
	tail := func(table byte) []byte { return []byte{0x20, 0, 0x13, 0, table, 0x0b} }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 2}, []byte{0x70, 0x05, 3, 3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("seed0", byte(wasm.ExternFunc), 1), wasmtest.ExportEntry("seed1", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("tail0", byte(wasm.ExternFunc), 3), wasmtest.ExportEntry("tail1", byte(wasm.ExternFunc), 4),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0), wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(declaredTarget)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}), wasmtest.Code(seed(0)), wasmtest.Code(seed(1)), wasmtest.Code(tail(0)), wasmtest.Code(tail(1)),
		)),
	)
}

func TestProductionIndexedTable64TailPlans(t *testing.T) {
	opts := CompileOptions{NativeTailCalls: true, InvocationContext: true, Interruptible: true, ForeignTableCalls: true, DeferCodeMapping: true}
	a, err := CompileModuleWith(localTailDecode(t, indexedTable64TailSource()), opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CompileModuleWith(localTailDecode(t, indexedTable64TailSource()), opts)
	if err != nil {
		t.Fatal(err)
	}
	if a.CodeImage != nil || a.NativeTail == nil || len(a.NativeTail.Plans) != 2 || !bytes.Equal(a.Code, b.Code) || !slices.Equal(a.NativeTail.Plans, b.NativeTail.Plans) {
		t.Fatal("indexed table64 tails are not address-free and deterministic")
	}
	for i, p := range a.NativeTail.Plans {
		if p.Kind != 1 || p.TypeIndex != 0 || p.FunctionIndex != uint32(i+3) || p.ImportIndex != 0 || p.SourcePC != 3 || p.ReferenceBase < p.SourceBase || p.ReferenceBase > p.FrameBytes-8 || p.SourceStride != 8 {
			t.Fatal("indexed table64 tail plan", i, p)
		}
	}
}
