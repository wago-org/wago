//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestTableMutationHints(t *testing.T) {
	body := []byte{
		0x41, 0x00, // i32.const 0
		0xd0, 0x70, // ref.null func
		0x26, 0x00, // table.set 0
		0x0b,
	}
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatalf("scanBodyBytes: %v", err)
	}
	if !h.mutatesTable {
		t.Fatal("table.set was not recorded as a table mutation")
	}

	ast := wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrTableGrow}}}
	if h := scanBody(ast, 0, 0, 0); !h.mutatesTable {
		t.Fatal("AST table.grow was not recorded as a table mutation")
	}
}

func TestImmutableLocalTableCallIndirectSpecialization(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00} // active elem: table[0] = func 0
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if got := stats.Funcs[1].Peephole["immutable-local-call-indirect"]; got != 1 {
		t.Fatalf("specialization count = %d, want 1", got)
	}
	if got := stats.Funcs[1].Peephole["immutable-table-type-check-elide"]; got != 1 {
		t.Fatalf("type-check elision count = %d, want 1", got)
	}

	m.Exports = append(m.Exports, wasm.Export{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}})
	hints, _, err := computeModuleHints(m, m.GlobalCount(), m.ImportedFuncCount())
	if err != nil {
		t.Fatalf("exported-table hints: %v", err)
	}
	for i := range hints {
		if hints[i].immutableLocalTable {
			t.Fatalf("function %d specialized an externally mutable exported table", i)
		}
	}
}

func TestImmutableLocalTableMixedTypesKeepDynamicCheck(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x02, 0x00, 0x01}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(i32, i32),
			wasmtest.FuncType(nil, i32),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := immutableLocalTableType(m); ok {
		t.Fatal("mixed-signature table was classified as uniform")
	}
}
