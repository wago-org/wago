//go:build arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestFuncHintsSizeArm64(t *testing.T) {
	const want = 200
	if got := unsafe.Sizeof(funcHints{}); got != want {
		t.Fatalf("funcHints size = %d, want %d", got, want)
	}
}

func TestModuleEffectsTransitiveArm64(t *testing.T) {
	functions := wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))
	memory := wasmtest.Vec([]byte{0x00, 0x01})
	global := wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))
	codes := wasmtest.Vec(
		wasmtest.Code([]byte{0x10, 0x01, 0x0b}),                   // 0 -> 1
		wasmtest.Code([]byte{0x10, 0x02, 0x0b}),                   // 1 -> 2
		wasmtest.Code([]byte{0x41, 0x00, 0x40, 0x00, 0x1a, 0x0b}), // memory.grow
		wasmtest.Code([]byte{0x41, 0x00, 0x24, 0x00, 0x0b}),       // global.set
	)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, functions),
		wasmtest.Section(5, memory),
		wasmtest.Section(6, global),
		wasmtest.Section(10, codes),
	)
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	var effects []shared.FuncEffects
	if _, _, err = computeModuleHintsWithPolicyAndEffects(m, m.GlobalCount(), m.ImportedFuncCount(), currentCodegenPolicy(), &effects); err != nil {
		t.Fatal(err)
	}
	want := []shared.FuncEffects{shared.EffectGrowsMemory, shared.EffectGrowsMemory, shared.EffectGrowsMemory, shared.EffectWritesGlobals}
	for i := range want {
		if effects[i] != want[i] {
			t.Fatalf("effects[%d] = %02b, want %02b", i, effects[i], want[i])
		}
	}
}

func TestModuleEffectsBoundedFallbackArm64(t *testing.T) {
	large := newModuleEffectCollector(maxEffectGraphFunctions+1, 0, 0)
	large.Begin(0)
	large.Call(0, 1)
	if got := large.Finish()[0]; got != shared.AllFuncEffects {
		t.Fatalf("large graph caller effects = %02b, want all", got)
	}

	overflow := newModuleEffectCollector(2, 0, maxEffectGraphCalls)
	overflow.Begin(0)
	for range maxEffectGraphCalls + 1 {
		overflow.Call(0, 1)
	}
	if got := overflow.Finish()[0]; got != shared.AllFuncEffects {
		t.Fatalf("edge-cap caller effects = %02b, want all", got)
	}
}

func TestScanBodyBytesUnobservableInitialLocalsARM64(t *testing.T) {
	body := []byte{
		0x20, 0x00, 0x1a, // local.get 0; drop: initial value is observable
		0x41, 0x01, 0x21, 0x01, // local 1 first set in entry prefix
		0x02, 0x40, // block
		0x41, 0x02, 0x21, 0x02, // local 2 set under control, but never read
		0x0b, 0x0b,
	}
	h, err := scanBodyBytes(body, 3, 0, 0)
	if err != nil {
		t.Fatalf("scanBodyBytes: %v", err)
	}
	if got, want := h.entryInitialized, uint64(1)<<1|uint64(1)<<2; got != want {
		t.Fatalf("entryInitialized = %#x, want %#x", got, want)
	}
}

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

func TestGCHelperHintScannersMarkNativeCalls(t *testing.T) {
	body := []byte{0x41, 0x00, 0xfb, 0x07, 0x00, 0x1a, 0x0b} // array.new_default 0; drop
	byteHints, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	astHints := scanBody(wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrI32Const},
		{Kind: wasm.InstrArrayNewDefault, Index: 0},
		{Kind: wasm.InstrDrop},
	}}, 0, 0, 0)
	if !byteHints.hasCall || !astHints.hasCall {
		t.Fatalf("array helper call hints byte/AST = %v/%v, want true/true", byteHints.hasCall, astHints.hasCall)
	}
}

func TestASTExceptionHintsReserveHandlerState(t *testing.T) {
	ast := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrTryTable},
		{Kind: wasm.InstrArrayNewDefault, Index: 0},
	}}
	h := scanBody(ast, 0, 0, 0)
	if !h.moduleEH || !h.hasControlFlow || !h.hasCall {
		t.Fatalf("AST exception hints = EH:%v control:%v call:%v, want all true", h.moduleEH, h.hasControlFlow, h.hasCall)
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
		{Kind: wasm.InstrBrOnNull},
		{Kind: wasm.InstrBrOnCast},
		{Kind: wasm.InstrTryTable},
		{Kind: wasm.InstrGlobalGet},
		{Kind: wasm.InstrI32Load},
	}, &facts)
	if facts.calleeCount != 1 || len(facts.callees) != 1 || facts.callees[0] != 3 ||
		!facts.hasControlCall || !facts.hasControlFlow || !facts.moduleEH || !facts.touchesGlobal || !facts.touchesMem {
		t.Fatalf("inline facts = %#v", facts)
	}
}

func TestInlineBoundaryParityBytesArm64(t *testing.T) {
	for _, op := range []byte{0xd5, 0xd6} {
		body := []byte{op, 0x00, 0x0b}
		h, err := scanBodyBytes(body, 0, 0, 0)
		if err != nil {
			t.Fatalf("production scan opcode %#x: %v", op, err)
		}
		var facts inlineFacts
		if err := scanInlineFactsBytes(body, &facts); err != nil {
			t.Fatalf("inline scan opcode %#x: %v", op, err)
		}
		if !h.hasControlFlow || !facts.hasControlFlow {
			t.Fatalf("opcode %#x control classification: production=%v inline=%v", op, h.hasControlFlow, facts.hasControlFlow)
		}
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
	if got, want := likelyThen.localScore[0], uint32(branchHintWeight); got != want {
		t.Fatalf("likely then local score = %d, want %d", got, want)
	}
	if got := likelyThen.localScore[1]; got != 1 {
		t.Fatalf("unlikely else local score = %d, want 1", got)
	}
	likelyElse, err := scanBodyBytesWithHints(body, 0, 2, 0, 0, []wasm.BranchHint{{Offset: 0, Likely: false}})
	if err != nil {
		t.Fatalf("scan likely-else: %v", err)
	}
	if got, want := likelyElse.localScore[1], uint32(branchHintWeight); got != want {
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
