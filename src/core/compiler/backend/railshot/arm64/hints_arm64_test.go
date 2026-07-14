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

func TestModuleGlobalScores(t *testing.T) {
	bytes := []byte{
		0x23, 0x00, // global.get 0
		0x24, 0x01, // global.set 1
		0x03, 0x40, // loop
		0x23, 0x00, // global.get 0
		0x24, 0x02, // global.set 2
		0x0b,
		0x02, 0x40, // block
		0x23, 0x01, // global.get 1
		0x0b,
		0x04, 0x40, // if
		0x23, 0x02, // global.get 2
		0x05,       // else
		0x24, 0x00, // global.set 0
		0x0b,
		0x1f, 0x40, 0x00, // try_table with no catches
		0x23, 0x01, // global.get 1
		0x0b,
		0x0b,
	}
	mod := &wasm.Module{Code: []wasm.Func{
		{BodyBytes: bytes},
		{Body: wasm.Expr{Instrs: []wasm.Instruction{
			{Kind: wasm.InstrGlobalGet, Index: 0},
			{Kind: wasm.InstrGlobalSet, Index: 2},
		}}},
	}}
	got, err := computeModuleGlobalScores(mod, 3)
	if err != nil {
		t.Fatalf("compute global scores: %v", err)
	}
	want := []int64{14, 4, 23}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("global %d score = %d, want %d", i, got[i], want[i])
		}
	}
	if scores, err := computeModuleGlobalScores(&wasm.Module{}, 3); err != nil || scores != nil {
		t.Fatalf("empty code scores = %v, %v", scores, err)
	}
	if scores, err := computeModuleGlobalScores(mod, 0); err != nil || scores != nil {
		t.Fatalf("zero-global scores = %v, %v", scores, err)
	}
	if _, err := computeModuleGlobalScores(&wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0x05}}}}, 1); err == nil {
		t.Fatal("malformed global-score body was accepted")
	}
}

func TestScanInlineFactsAST(t *testing.T) {
	facts := inlineFacts{}
	scanInlineFactsAST([]wasm.Instruction{
		{Kind: wasm.InstrCall, Index: 3},
		{Kind: wasm.InstrCallIndirect},
		{Kind: wasm.InstrBrIf},
		{Kind: wasm.InstrGlobalGet},
		{Kind: wasm.InstrI32Load},
	}, &facts)
	if facts.calleeCount != 1 || len(facts.callees) != 1 || facts.callees[0] != 3 ||
		!facts.hasControlCall || !facts.hasControlFlow || !facts.touchesGlobal || !facts.touchesMem {
		t.Fatalf("inline facts = %#v", facts)
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
