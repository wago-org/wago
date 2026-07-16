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

func TestLoopHintReservesLoopScratchPins(t *testing.T) {
	h, err := scanBodyBytes([]byte{0x03, 0x40, 0x0b, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatalf("scanBodyBytes: %v", err)
	}
	if !h.hasLoop {
		t.Fatal("structured loop was not recorded")
	}
	straight, err := scanBodyBytes([]byte{0x01, 0x0b}, 0, 0, 0) // nop; end
	if err != nil {
		t.Fatalf("straight scanBodyBytes: %v", err)
	}
	if straight.hasLoop {
		t.Fatal("straight-line body was classified as a loop")
	}
}

func TestBranchHintWeightsIfArmLocalScores(t *testing.T) {
	body := []byte{
		0x04, 0x40, // if
		0x20, 0x00, 0x1a, // then: local.get 0; drop
		0x05,
		0x20, 0x01, 0x1a, // else: local.get 1; drop
		0x0b, 0x0b,
	}
	likelyThen, err := scanBodyBytesWithHints(body, 0, 2, 0, 0, []wasm.BranchHint{{Offset: 0, Likely: true}})
	if err != nil {
		t.Fatalf("scan likely-then: %v", err)
	}
	if got, want := likelyThen.localScore[0], int64(branchHintWeight); got != want {
		t.Fatalf("likely then local score = %d, want %d", got, want)
	}
	if got := likelyThen.localScore[1]; got != 1 {
		t.Fatalf("unlikely else local score = %d, want 1", got)
	}
	likelyElse, err := scanBodyBytesWithHints(body, 0, 2, 0, 0, []wasm.BranchHint{{Offset: 0, Likely: false}})
	if err != nil {
		t.Fatalf("scan likely-else: %v", err)
	}
	if got, want := likelyElse.localScore[1], int64(branchHintWeight); got != want {
		t.Fatalf("likely else local score = %d, want %d", got, want)
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
	if got := stats.Funcs[1].Peephole["monomorphic-call-indirect"]; got != 1 {
		t.Fatalf("monomorphic specialization count = %d, want 1", got)
	}
	if got := stats.Funcs[1].Peephole["immutable-local-call-indirect"]; got != 0 {
		t.Fatalf("generic immutable specialization count = %d, want 0", got)
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
