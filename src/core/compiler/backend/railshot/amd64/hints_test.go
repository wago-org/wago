//go:build linux && amd64

package amd64

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestScanBodyHints(t *testing.T) {
	callOnly := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet},
		{Kind: wasm.InstrCall, Index: 7},
	}}
	h := scanBody(callOnly, 1, 0, 7)
	if !h.hasCall || h.touchesMemory || !h.callsSelf {
		t.Fatalf("call-only body: hasCall=%v touchesMemory=%v callsSelf=%v", h.hasCall, h.touchesMemory, h.callsSelf)
	}
	if h.localScore[0] != 1 {
		t.Fatalf("local 0 score = %d, want 1", h.localScore[0])
	}
	if h2 := scanBody(callOnly, 1, 0, 8); h2.callsSelf {
		t.Fatal("call to 7 should not count as self for index 8")
	}

	callMemory := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet},
		{Kind: wasm.InstrI32Load},
		{Kind: wasm.InstrCall},
		{Kind: wasm.InstrMemoryFill},
	}}
	h = scanBody(callMemory, 1, 0, 99)
	if !h.hasCall || !h.touchesMemory || !h.usesBulkMem {
		t.Fatalf("call+memory body: %+v", h)
	}
}

func TestScanBodyBytesCallHints(t *testing.T) {
	h, err := scanBodyBytes([]byte{0x10, 0x07, 0x0b}, 0, 0, 7) // call 7; end
	if err != nil {
		t.Fatalf("scan self call: %v", err)
	}
	if !h.hasCall || !h.callsSelf {
		t.Fatalf("self call hints = %+v, want hasCall and callsSelf", h)
	}

	h, err = scanBodyBytes([]byte{0x11, 0x00, 0x00, 0x0b}, 0, 0, 0) // call_indirect type 0 table 0; end
	if err != nil {
		t.Fatalf("scan call_indirect: %v", err)
	}
	if !h.hasCall || h.callsSelf {
		t.Fatalf("call_indirect hints = %+v, want hasCall without callsSelf", h)
	}
}

func TestScanBodyBytesStackArenaHintSkipsSIMDStores(t *testing.T) {
	body := []byte{
		0xfd, 0x0b, 0x04, 0x00, // v128.store align=16 offset=0
		0xfd, 0x58, 0x00, 0x00, 0x0f, // v128.store8_lane align=1 offset=0 lane=15
		0xfd, 0x59, 0x01, 0x00, 0x07, // v128.store16_lane align=2 offset=0 lane=7
		0xfd, 0x5a, 0x02, 0x00, 0x03, // v128.store32_lane align=4 offset=0 lane=3
		0xfd, 0x5b, 0x03, 0x00, 0x01, // v128.store64_lane align=8 offset=0 lane=1
		0x0b,
	}
	endOnly, err := scanBodyBytes([]byte{0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatalf("scan end-only body: %v", err)
	}
	storeHints, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatalf("scan SIMD stores: %v", err)
	}
	if storeHints.stackArenaNodes != endOnly.stackArenaNodes {
		t.Fatalf("SIMD store stack arena nodes = %d, want end-only baseline %d", storeHints.stackArenaNodes, endOnly.stackArenaNodes)
	}

	body = []byte{
		0xfd, 0x54, 0x00, 0x00, 0x0f, // v128.load8_lane align=1 offset=0 lane=15
		0x0b,
	}
	loadHints, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatalf("scan SIMD load lane: %v", err)
	}
	if loadHints.stackArenaNodes != endOnly.stackArenaNodes+1 {
		t.Fatalf("SIMD load-lane stack arena nodes = %d, want %d", loadHints.stackArenaNodes, endOnly.stackArenaNodes+1)
	}
}

func TestScanBodyBytesStackArenaHintSkipsSIMDImmediateBytes(t *testing.T) {
	m := benchSIMDHeavyModule(t)
	ft, ok := m.LocalFuncType(0)
	if !ok {
		t.Fatal("missing benchmark function type")
	}
	nLocals, err := countLocals(ft.Params, m.Code[0].Locals)
	if err != nil {
		t.Fatalf("count locals: %v", err)
	}
	h, err := scanFuncBody(m.Code[0], nLocals, m.GlobalCount(), uint32(m.ImportedFuncCount()), nil)
	if err != nil {
		t.Fatalf("scanFuncBody: %v", err)
	}
	legacy := stackArenaCapForBody(len(m.Code[0].BodyBytes), nLocals)
	hinted := stackArenaCapForHints(len(m.Code[0].BodyBytes), nLocals, h.stackArenaNodes)
	if h.stackArenaNodes == 0 || h.stackArenaNodes >= len(m.Code[0].BodyBytes)/2 {
		t.Fatalf("stack arena node hint = %d, body bytes = %d", h.stackArenaNodes, len(m.Code[0].BodyBytes))
	}
	if hinted >= legacy {
		t.Fatalf("hinted stack arena cap = %d, want less than legacy %d", hinted, legacy)
	}
}

func TestScanBodyBytesMemoryHints(t *testing.T) {
	body := []byte{
		0x28, 0x02, 0x00, // i32.load align=2 offset=0
		0x36, 0x02, 0x00, // i32.store align=2 offset=0
		0x3f, 0x00, // memory.size 0
		0x40, 0x00, // memory.grow 0
		0x0b,
	}
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatalf("scan memory body: %v", err)
	}
	if !h.touchesMemory || h.usesBulkMem {
		t.Fatalf("memory hints = %+v, want touchesMemory only", h)
	}
}

func TestScanBodyBytesBulkMemoryHints(t *testing.T) {
	body := []byte{
		0xfc, 0x0a, 0x00, 0x00, // memory.copy dstmem=0 srcmem=0
		0xfc, 0x0b, 0x00, // memory.fill mem=0
		0x0b,
	}
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatalf("scan bulk memory body: %v", err)
	}
	if !h.touchesMemory || !h.usesBulkMem {
		t.Fatalf("bulk memory hints = %+v, want touchesMemory and usesBulkMem", h)
	}
}

func TestScanBodyBytesLoopWeightedScoresAndEligibility(t *testing.T) {
	body := []byte{
		0x03, 0x40, // loop void
		0x20, 0x00, // local.get 0
		0x21, 0x01, // local.set 1
		0x23, 0x01, // global.get 1
		0x24, 0x02, // global.set 2
		0x0b, // end loop
		0x0b, // end function
	}
	h, err := scanBodyBytes(body, 2, 3, 0)
	if err != nil {
		t.Fatalf("scan loop body: %v", err)
	}
	if h.localScore[0] != 10 || h.localScore[1] != 20 {
		t.Fatalf("local scores = %v, want [10 20]", h.localScore)
	}
	if h.globalScore[1] != 10 || h.globalScore[2] != 20 {
		t.Fatalf("global scores = %v, want g1=10 g2=20", h.globalScore)
	}
	if !h.globalElig[1] || !h.globalElig[2] {
		t.Fatalf("global eligibility = %v, want globals 1 and 2 eligible", h.globalElig)
	}
}

func TestScanBodyBytesRepeatedGlobalsAreEligibleOncePerCallFreeLoop(t *testing.T) {
	body := []byte{
		0x03, 0x40, // loop void
		0x23, 0x00, // global.get 0
		0x23, 0x00, // global.get 0 again
		0x24, 0x00, // global.set 0
		0x0b,
		0x0b,
	}
	h, err := scanBodyBytes(body, 0, 1, 0)
	if err != nil {
		t.Fatalf("scan repeated global loop: %v", err)
	}
	if h.globalScore[0] != 40 { // two gets + one set, all at loop weight 10
		t.Fatalf("global score = %d, want 40", h.globalScore[0])
	}
	if !h.globalElig[0] {
		t.Fatalf("global eligibility = %v, want global 0 eligible", h.globalElig)
	}
}

func TestScanBodyBytesLoopWithCallDisablesGlobalEligibility(t *testing.T) {
	body := []byte{
		0x03, 0x40, // loop void
		0x23, 0x00, // global.get 0
		0x23, 0x00, // repeated global access in the same ineligible loop
		0x10, 0x01, // call 1
		0x0b, // end loop
		0x0b, // end function
	}
	h, err := scanBodyBytes(body, 0, 1, 0)
	if err != nil {
		t.Fatalf("scan loop call body: %v", err)
	}
	if !h.hasCall {
		t.Fatalf("hints = %+v, want hasCall", h)
	}
	if h.globalScore[0] == 0 {
		t.Fatalf("global scores = %v, want global 0 scored", h.globalScore)
	}
	if h.globalElig[0] {
		t.Fatalf("global eligibility = %v, call-containing loop should not be eligible", h.globalElig)
	}
}

func TestPickModuleGlobalsUsesAggregateScores(t *testing.T) {
	m := &wasm.Module{
		Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}},
		Code:    []wasm.Func{{}},
	}
	if pins := pickModuleGlobals(m, m.GlobalCount(), []int64{0}); len(pins) != 0 {
		t.Fatalf("pickModuleGlobals with zero aggregate score = %+v, want none", pins)
	}
	pins := pickModuleGlobals(m, m.GlobalCount(), []int64{3 * loopWeight(1)})
	if len(pins) != 1 || pins[0].global != 0 || pins[0].reg != moduleGlobalRegs[0] {
		t.Fatalf("pickModuleGlobals with hot aggregate score = %+v, want global 0 in first module register", pins)
	}
}

func TestModuleGlobalScoreScanMatchesFullHints(t *testing.T) {
	globals := []wasm.Global{
		{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}},
		{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}},
	}
	loopGet := []byte{0x03, 0x40, 0x23, 0x00, 0x0b, 0x0b}
	loopSet := []byte{0x03, 0x40, 0x24, 0x00, 0x0b, 0x0b}
	nestedLoopGet := []byte{0x03, 0x40, 0x03, 0x40, 0x23, 0x00, 0x0b, 0x0b, 0x0b}
	nonLoopBelowThreshold := []byte{0x23, 0x00, 0x24, 0x00, 0x23, 0x01, 0x0b}

	cases := []struct {
		name     string
		bodies   [][]byte
		wantPins int
	}{
		{name: "global.get in loop", bodies: [][]byte{loopGet, loopGet, loopGet}, wantPins: 1},
		{name: "global.set in loop", bodies: [][]byte{loopSet, loopSet}, wantPins: 1},
		{name: "nested loops", bodies: [][]byte{nestedLoopGet}, wantPins: 1},
		{name: "non-loop global access below threshold", bodies: [][]byte{nonLoopBelowThreshold}, wantPins: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &wasm.Module{Globals: globals}
			for _, body := range tc.bodies {
				m.Code = append(m.Code, wasm.Func{BodyBytes: body})
			}
			got, err := computeModuleGlobalScores(m, m.GlobalCount())
			if err != nil {
				t.Fatalf("compute module global scores: %v", err)
			}
			want := make([]int64, len(globals))
			for _, body := range tc.bodies {
				h, err := scanBodyBytes(body, 0, len(globals), 0)
				if err != nil {
					t.Fatalf("full scan body %x: %v", body, err)
				}
				for g, score := range h.globalScore {
					want[g] += score
				}
			}
			if len(got) != len(want) {
				t.Fatalf("aggregate len = %d, want %d", len(got), len(want))
			}
			for g := range want {
				if got[g] != want[g] {
					t.Fatalf("aggregate scores = %v, want %v", got, want)
				}
			}
			pins := pickModuleGlobals(m, m.GlobalCount(), got)
			if len(pins) != tc.wantPins {
				t.Fatalf("pins = %+v, want %d", pins, tc.wantPins)
			}
			if tc.wantPins != 0 && pins[0].global != 0 {
				t.Fatalf("pins = %+v, want global 0 hot", pins)
			}
		})
	}
}

func TestModuleGlobalScoreScanSupportsASTBodies(t *testing.T) {
	instrs := make([]wasm.Instruction, 15)
	for i := range instrs {
		instrs[i] = wasm.Instruction{Kind: wasm.InstrGlobalSet, Index: 0}
	}
	m := &wasm.Module{
		Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}},
		Code:    []wasm.Func{{Body: wasm.Expr{Instrs: instrs}}},
	}
	got, err := computeModuleGlobalScores(m, m.GlobalCount())
	if err != nil {
		t.Fatalf("compute module global scores for ast body: %v", err)
	}
	want := scanBody(m.Code[0].Body, 0, 1, 0).globalScore
	if len(got) != 1 || got[0] != want[0] || got[0] != 30 {
		t.Fatalf("AST aggregate scores = %v, want %v", got, want)
	}
	pins := pickModuleGlobals(m, m.GlobalCount(), got)
	if len(pins) != 1 || pins[0].global != 0 {
		t.Fatalf("AST module global pins = %+v, want global 0", pins)
	}
}

// computeModuleHints folds the module-wide global-scores pass into the single
// per-function hints scan. This pins that merged path to the standalone
// computeModuleGlobalScores oracle: same aggregate scores, and each cached
// funcHints must equal an independent computeFuncHints for that function.
func TestComputeModuleHintsMatchesGlobalScoreOracle(t *testing.T) {
	globals := make([]wasm.Global, 4)
	for i := range globals {
		globals[i] = wasm.Global{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}
	}
	// A few functions with different global/local access + loop nesting.
	bodies := [][]byte{
		{0x23, 0x00, 0x24, 0x01, 0x0b},                               // global.get 0; global.set 1; end
		{0x03, 0x40, 0x23, 0x02, 0x24, 0x02, 0x0b, 0x0b},             // loop { global.get 2; global.set 2 } end
		{0x20, 0x00, 0x21, 0x00, 0x23, 0x03, 0x1a, 0x0b},             // local.get 0; local.set 0; global.get 3; drop; end
		{0x02, 0x40, 0x23, 0x01, 0x1a, 0x0b, 0x23, 0x00, 0x1a, 0x0b}, // block { global.get 1; drop } global.get 0; drop; end
	}
	m := &wasm.Module{
		Types:   []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		Globals: globals,
	}
	for _, b := range bodies {
		m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{Index: 0})
		m.Code = append(m.Code, wasm.Func{BodyBytes: b, Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: 1, Type: wasm.I32}}}})
	}

	allHints, agg, err := computeModuleHints(m, m.GlobalCount(), 0)
	if err != nil {
		t.Fatalf("computeModuleHints: %v", err)
	}
	wantAgg, err := computeModuleGlobalScores(m, m.GlobalCount())
	if err != nil {
		t.Fatalf("computeModuleGlobalScores: %v", err)
	}
	if len(agg) != len(wantAgg) {
		t.Fatalf("agg len = %d, want %d", len(agg), len(wantAgg))
	}
	for g := range wantAgg {
		if agg[g] != wantAgg[g] {
			t.Fatalf("aggregate scores = %v, want %v", agg, wantAgg)
		}
	}
	for i := range m.Code {
		want, err := computeFuncHints(m, i, m.GlobalCount(), 0)
		if err != nil {
			t.Fatalf("computeFuncHints %d: %v", i, err)
		}
		if !reflect.DeepEqual(allHints[i], want) {
			t.Fatalf("func %d cached hints = %+v, want %+v", i, allHints[i], want)
		}
	}
}

func TestManyGlobalHintScoresEligibilityAndModulePinning(t *testing.T) {
	const hotGlobal = 123
	body := []byte{
		0x03, 0x40, // loop void
		0x23, hotGlobal, // global.get hotGlobal
		0x24, hotGlobal, // global.set hotGlobal
		0x0b,
		0x0b,
	}
	h, err := scanBodyBytes(body, 0, 256, 0)
	if err != nil {
		t.Fatalf("scan many-global body: %v", err)
	}
	if h.globalScore[hotGlobal] != 30 || !h.globalElig[hotGlobal] {
		t.Fatalf("hot global hints score=%d elig=%v, want score 30 and eligible", h.globalScore[hotGlobal], h.globalElig[hotGlobal])
	}
	globals := make([]wasm.Global, 256)
	for i := range globals {
		globals[i].Type = wasm.GlobalType{Type: wasm.I32, Mutable: true}
	}
	m := &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Globals:   globals,
		Code:      []wasm.Func{{BodyBytes: body}},
	}
	agg, err := computeModuleGlobalScores(m, m.GlobalCount())
	if err != nil {
		t.Fatalf("compute module global scores: %v", err)
	}
	if agg[hotGlobal] != 30 {
		t.Fatalf("aggregate score for global %d = %d, want 30", hotGlobal, agg[hotGlobal])
	}
	pins := pickModuleGlobals(m, m.GlobalCount(), agg)
	if len(pins) != 1 || pins[0].global != hotGlobal || pins[0].reg != moduleGlobalRegs[0] {
		t.Fatalf("module global pins = %+v, want global %d in first module register", pins, hotGlobal)
	}
}

func TestScanFuncBodyUsesDecodedBodyBytes(t *testing.T) {
	global := []byte{0x7f, 0x01, 0x41, 0x00, 0x0b} // (mut i32) = 0
	body := []byte{
		0x00,                                                 // no local decls
		0x03, 0x40, 0x20, 0x00, 0x1a, 0x23, 0x01, 0x1a, 0x0b, // loop1
		0x03, 0x40, 0x23, 0x02, 0x1a, 0x20, 0x00, 0x10, 0x00, 0x0b, // loop2 (self call)
		0x23, 0x00, 0x1a, // global.get 0; drop
		0x0b,
	}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(global, global, global)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Code[0].Body.Instrs) != 0 || len(m.Code[0].BodyBytes) == 0 {
		t.Fatalf("decoded module should be byte-backed: instrs=%d bytes=%x", len(m.Code[0].Body.Instrs), m.Code[0].BodyBytes)
	}
	h, err := scanFuncBody(m.Code[0], 1, 3, 0, nil)
	if err != nil {
		t.Fatalf("scan decoded body: %v", err)
	}
	if !h.hasCall || !h.callsSelf {
		t.Fatalf("decoded recursive body hints = %+v, want call+self-call", h)
	}
	if h.localScore[0] == 0 || h.globalScore[0] == 0 || h.globalScore[1] == 0 || h.globalScore[2] == 0 {
		t.Fatalf("decoded byte-backed body produced missing scores: locals=%v globals=%v", h.localScore, h.globalScore)
	}
	if !h.globalElig[1] {
		t.Fatalf("decoded loop without call should mark global 1 eligible: %v", h.globalElig)
	}
	if h.globalElig[2] {
		t.Fatalf("decoded loop with self call should not mark global 2 eligible: %v", h.globalElig)
	}
}

func TestDecodedRecursiveBodyDoesNotSkipStackFence(t *testing.T) {
	body := []byte{0x00, 0x10, 0x00, 0x0b} // no locals; call function 0; end
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode recursive module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("validate recursive module: %v", err)
	}
	h, err := scanFuncBody(m.Code[0], 0, 0, 0, nil)
	if err != nil {
		t.Fatalf("scan recursive body: %v", err)
	}
	if !h.hasCall || !h.callsSelf {
		t.Fatalf("recursive decoded body hints = %+v, want hasCall and callsSelf", h)
	}
	if shouldSkipStackFence(h.hasCall, 0, len(m.Code[0].BodyBytes)) {
		t.Fatalf("recursive call-making body was allowed to skip the stack fence")
	}
}

func TestScanBodyBytesMalformedImmediateReturnsError(t *testing.T) {
	if _, err := scanBodyBytes([]byte{0x10, 0x80, 0x0b}, 0, 0, 0); err == nil {
		t.Fatal("scan malformed call immediate succeeded, want error")
	}
}
