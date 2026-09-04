//go:build arm64

package arm64

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestParallelModuleHintsMatchSerialArm64(t *testing.T) {
	for _, name := range []string{"json-as-simd.wasm", "lua.wasm", "sqlite3.wasm"} {
		t.Run(name, func(t *testing.T) {
			m := readParallelTestModuleArm64(t, "../../../../../../bench/corpus/"+name)
			policy := currentCodegenPolicy()
			serial, serialSidecar, serialGlobals, err := computeModuleHintsWithWorkersPolicy(m, m.GlobalCount(), m.ImportedFuncCount(), 1, policy)
			if err != nil {
				t.Fatal(err)
			}
			parallel, parallelSidecar, parallelGlobals, err := computeModuleHintsWithWorkersPolicy(m, m.GlobalCount(), m.ImportedFuncCount(), 4, policy)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(parallel, serial) {
				for i := range serial {
					if parallel[i] != serial[i] {
						t.Fatalf("function %d hints differ:\nparallel: %#v\nserial:   %#v", i, parallel[i], serial[i])
					}
				}
			}
			if !reflect.DeepEqual(parallelSidecar, serialSidecar) {
				t.Fatalf("sidecars differ: parallel scores=%d last=%d globals=%d; serial scores=%d last=%d globals=%d", len(parallelSidecar.localScore), len(parallelSidecar.localLastGet), len(parallelSidecar.sparseGlobals), len(serialSidecar.localScore), len(serialSidecar.localLastGet), len(serialSidecar.sparseGlobals))
			}
			if !reflect.DeepEqual(parallelGlobals, serialGlobals) {
				t.Fatal("module global scores differ")
			}
		})
	}
}

func TestFuncHintsSizeArm64(t *testing.T) {
	const want = 32
	if got := unsafe.Sizeof(funcHints{}); got != want {
		t.Fatalf("funcHints size = %d, want %d", got, want)
	}
}

func TestImmediateFreeStackArenaHintParityArm64(t *testing.T) {
	for raw := 0; raw < 256; raw++ {
		op := byte(raw)
		if _, ok := wasm.ImmediateFreeInstructionKind(op); !ok {
			continue
		}
		old := byteBodyScanner{r: byteScanReader{Reader: wasm.ReaderFrom([]byte{0x21})}}
		var imm wasm.InstructionImmediate
		old.noteStackArenaOp(op, &imm)
		fast := byteBodyScanner{r: byteScanReader{Reader: wasm.ReaderFrom([]byte{0x21})}}
		fast.noteImmediateFreeStackArenaOp(op)
		if old.h.stackArenaNodes != fast.h.stackArenaNodes || old.h.flags != fast.h.flags {
			t.Fatalf("opcode %#x: fast hint = {nodes:%d flags:%#x}, want {nodes:%d flags:%#x}",
				op, fast.h.stackArenaNodes, fast.h.flags, old.h.stackArenaNodes, old.h.flags)
		}
	}
}

func TestSerialLocalScratchCapacityUsesTotalLocalCountArm64(t *testing.T) {
	params := make([]wasm.ValType, 96)
	for i := range params {
		params[i] = wasm.I64
	}
	m := modFuncs(t,
		funcDef{body: []byte{0x00, 0x0b}},
		funcDef{params: params, body: []byte{0x01, 0x07, 0x7e, 0x0b}},
		funcDef{params: params[:16], body: []byte{0x00, 0x0b}},
	)
	hints, _, _, err := computeModuleHints(m, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := serialLocalScratchCapacity(hints, inlineTargetTable{}, make([]bool, len(m.Code))), 103; got != want {
		t.Fatalf("serial local scratch capacity = %d, want total parameter-plus-local count %d", got, want)
	}
}

func TestTaglessExceptionHandlingOmitsIntervalSidecarsArm64(t *testing.T) {
	body := []byte{0x01, 0x80, 0x01, 0x7f} // 128 i32 locals.
	body = append(body, make([]byte, 128)...)
	body = append(body, 0x1f, 0x40, 0x01, byte(wasm.CatchAll), 0x00, 0x0b, 0x0b)
	m := modFuncs(t, funcDef{body: body})
	hints, sidecar, _, err := computeModuleHints(m, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	view := sidecar.view(hints[0])
	if !hints[0].flags.has(hintModuleEH) {
		t.Fatal("tagless try_table module was not classified as exception handling")
	}
	if hints[0].flags.has(hintIntervalRegionStorage) || len(view.localLastGet) != 0 || len(view.localScore) != 64 {
		t.Fatalf("tagless EH interval sidecars = interval:%v scores:%d last-gets:%d, want false/64/0", hints[0].flags.has(hintIntervalRegionStorage), len(view.localScore), len(view.localLastGet))
	}
}

func TestDirectCalleePreservesPinsUsesRetainedHint(t *testing.T) {
	hints := make([]funcHints, 2)
	hints[1].flags.set(hintPreservesCallerPins)
	f := fn{calleeHints: hints}
	if f.directCalleePreservesPins(0) {
		t.Fatal("unmarked callee preserves pins")
	}
	if !f.directCalleePreservesPins(1) {
		t.Fatal("marked callee does not preserve pins")
	}
	if f.directCalleePreservesPins(-1) || f.directCalleePreservesPins(len(hints)) {
		t.Fatal("out-of-range callee preserves pins")
	}
}

func TestScanBodyBytesFloatConstHintArm64(t *testing.T) {
	integer, err := scanBodyBytes([]byte{0x41, 0x00, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if integer.flags.has(hintHasFloatConst) {
		t.Fatal("integer body reported a float constant")
	}
	for _, body := range [][]byte{
		{0x43, 0, 0, 0, 0, 0x0b},
		{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b},
	} {
		h, err := scanBodyBytes(body, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !h.flags.has(hintHasFloatConst) {
			t.Fatalf("float body %x did not report a float constant", body)
		}
	}
	decoded := scanBody(wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrF32Const}}}, 0, 0, 0)
	if !decoded.flags.has(hintHasFloatConst) {
		t.Fatal("decoded float body did not report a float constant")
	}
}

func TestControlDepthHintCountsNestedFramesArm64(t *testing.T) {
	h, err := scanBodyBytes([]byte{
		0x02, 0x40, // block
		0x04, 0x40, // if
		0x03, 0x40, // loop
		0x0e, 0x01, 0x00, 0x00, // br_table 0 0; does not open a frame
		0x0b, 0x05, 0x0b, 0x0b, 0x0b,
	}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.maxControlDepth != 3 {
		t.Fatalf("max control depth = %d, want 3", h.maxControlDepth)
	}
}

func TestControlDepthHintSaturatesArm64(t *testing.T) {
	var h funcHints
	h.noteControlDepth(254)
	h.noteControlDepth(300)
	if h.maxControlDepth != 255 {
		t.Fatalf("saturated max control depth = %d, want 255", h.maxControlDepth)
	}
}

func TestControlDepthHintLeavesStraightLineLazyArm64(t *testing.T) {
	h, err := scanBodyBytes([]byte{0x41, 0x00, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.maxControlDepth != 0 {
		t.Fatalf("max control depth = %d, want 0", h.maxControlDepth)
	}
}

func TestModuleHintsCountLocalDirectCallRelocations(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: []wasm.TypeIdx{
			{Index: 0},
			{Index: 0},
		},
		Code: []wasm.Func{
			{BodyBytes: []byte{0x10, 0x01, 0x10, 0x01, 0x0b}},
			{BodyBytes: []byte{0x0b}},
		},
	}
	hints, _, _, err := computeModuleHints(m, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := hints[0].callRelocSites; got != 2 {
		t.Fatalf("call relocation sites = %d, want 2", got)
	}
}

func TestEntryInitializedSharesLocalScoreStorageArm64(t *testing.T) {
	scores := make([]uint32, 2)
	h := funcHintsWithStorage(scores)
	h.markEntryInitialized(1)
	if h.entryInitialized != uint64(1)<<1 {
		t.Fatalf("scan-local entry initialized = %#x, want bit 1", h.entryInitialized)
	}
	addHotness(scores, 1, 7)
	if got := localHotness(scores[1]); got != 7 {
		t.Fatalf("local hotness = %d, want 7", got)
	}
	scores[1] = localScoreEntryInitialized | localScoreHotnessMask
	addHotness(scores, 1, 1)
	if got := scores[1]; got != localScoreEntryInitialized|localScoreHotnessMask {
		t.Fatalf("saturated packed score = %#x", got)
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
	if !h.flags.has(hintMutatesTable) {
		t.Fatal("table.set was not recorded as a table mutation")
	}

	ast := wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrTableGrow}}}
	if h := scanBody(ast, 0, 0, 0); !h.flags.has(hintMutatesTable) {
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
	if !byteHints.flags.has(hintHasCall) || !astHints.flags.has(hintHasCall) {
		t.Fatalf("array helper call hints byte/AST = %v/%v, want true/true", byteHints.flags.has(hintHasCall), astHints.flags.has(hintHasCall))
	}
}

func TestASTExceptionHintsReserveHandlerState(t *testing.T) {
	ast := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrTryTable},
		{Kind: wasm.InstrArrayNewDefault, Index: 0},
	}}
	h := scanBody(ast, 0, 0, 0)
	if !h.flags.has(hintModuleEH) || !h.flags.has(hintHasControlFlow) || !h.flags.has(hintHasCall) {
		t.Fatalf("AST exception hints = EH:%v control:%v call:%v, want all true", h.flags.has(hintModuleEH), h.flags.has(hintHasControlFlow), h.flags.has(hintHasCall))
	}
}

func TestLoopHintReservesLoopScratchPins(t *testing.T) {
	h, err := scanBodyBytes([]byte{0x03, 0x40, 0x0b, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatalf("scanBodyBytes: %v", err)
	}
	if !h.flags.has(hintHasLoop) {
		t.Fatal("structured loop was not recorded")
	}
	straight, err := scanBodyBytes([]byte{0x01, 0x0b}, 0, 0, 0) // nop; end
	if err != nil {
		t.Fatalf("straight scanBodyBytes: %v", err)
	}
	if straight.flags.has(hintHasLoop) {
		t.Fatal("straight-line body was classified as a loop")
	}
}

func TestScanBodyBytesStackArenaHintCountsAtomicsArm64(t *testing.T) {
	for _, kind := range []wasm.InstrKind{wasm.InstrAtomicFence, wasm.InstrI32AtomicStore, wasm.InstrI64AtomicStore32} {
		if stackArenaOpAllocates(0xfe, &wasm.InstructionImmediate{Kind: kind}) {
			t.Fatalf("result-free atomic %v counted as arena allocation", kind)
		}
	}
	endOnly, err := scanBodyBytes([]byte{0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{
		0x41, 0x00, // i32.const address
		0xfe, 0x10, 0x02, 0x00, // i32.atomic.load align=4 offset=0
		0x1a, 0x0b, // drop; end
	}
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := endOnly.stackArenaNodes + 2; h.stackArenaNodes != want {
		t.Fatalf("atomic stack arena nodes = %d, want %d", h.stackArenaNodes, want)
	}
}

func TestScanBodyBytesDiscountsAlgebraicIdentitiesArm64(t *testing.T) {
	body := []byte{
		0x20, 0x00, 0x41, 0x00, 0x6a, 0x1a, // x + 0; drop
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x1a, // x + 1; drop (not an identity)
		0x20, 0x00, 0x20, 0x00, 0x6b, 0x1a, // x - x; drop
		0x20, 0x00, 0x41, 0x20, 0x74, 0x1a, // i32.shl by 32; drop
		0x20, 0x00, 0x42, 0xc0, 0x00, 0x86, 0x1a, // i64.shl by 64; drop
		0x0b,
	}
	h, err := scanBodyBytes(body, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.stackArenaDiscount != 3 {
		t.Fatalf("algebraic discount = %d, want 3", h.stackArenaDiscount)
	}
	if !h.flags.has(hintHasStackSinkFusion) {
		t.Fatal("multibyte identity constant did not retain legacy sizing")
	}
}

func TestScanBodyBytesDetectsDeadCodeAfterTerminatorArm64(t *testing.T) {
	h, err := scanBodyBytes([]byte{0x00, 0x41, 0x00, 0x1a, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !h.flags.has(hintHasStackSinkFusion) {
		t.Fatal("dead instructions after unreachable were not detected")
	}
	terminalOnly, err := scanBodyBytes([]byte{0x00, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if terminalOnly.flags.has(hintHasStackSinkFusion) {
		t.Fatal("terminal unreachable was marked as followed by dead code")
	}
}

func TestScanBodyBytesDiscountsSWARLookaheadCandidatesArm64(t *testing.T) {
	body := []byte{
		0x20, 0x00, 0x42, 0x00, 0x83, 0x22, 0x01, 0x1a, // i64.and; local.tee
		0x20, 0x00, 0x42, 0x20, 0x88, 0x22, 0x01, 0x1a, // i64.shr_u; local.tee
		0x0b,
	}
	h, err := scanBodyBytes(body, 2, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.stackArenaDiscount != 44 {
		t.Fatalf("SWAR lookahead discount = %d, want 44", h.stackArenaDiscount)
	}
}

func TestScanBodyBytesDetectsSIMDFusionRiskArm64(t *testing.T) {
	body := []byte{
		0xfd, 0x0b, 0x04, 0x00, // v128.store align=16 offset=0
		0x0b,
	}
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !h.flags.has(hintHasStackSinkFusion) {
		t.Fatal("SIMD body did not select legacy arena sizing")
	}
}

func TestScanBodyBytesDetectsStackSinkFusionArm64(t *testing.T) {
	body := []byte{
		0x20, 0x00, // local.get 0
		0x20, 0x01, // local.get 1
		0x92,       // f32.add
		0x21, 0x02, // local.set 2
		0x0b,
	}
	h, err := scanBodyBytes(body, 3, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !h.flags.has(hintHasStackSinkFusion) {
		t.Fatal("float local sink fusion was not detected")
	}
}

func TestScanBodyBytesStackArenaHintCountsReferenceResultsArm64(t *testing.T) {
	endOnly, err := scanBodyBytes([]byte{0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{
		0x20, 0x00, // local.get 0
		0xd4,       // ref.as_non_null
		0x1a,       // drop
		0x41, 0x00, // i32.const 0
		0xfb, 0x1c, // ref.i31
		0x1a, 0x0b, // drop; end
	}
	h, err := scanBodyBytes(body, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := endOnly.stackArenaNodes + 4; h.stackArenaNodes != want {
		t.Fatalf("reference stack arena nodes = %d, want %d", h.stackArenaNodes, want)
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

func TestGlobalScoreScannerConsumesMixedMemoryWidthImmediateArm64(t *testing.T) {
	body := []byte{
		0x42, 0x00, // i64.const 0
		0xfd, 0x00, 0x44, 0x01, 0x80, 0x80, 0x80, 0x80, 0x10, // v128.load memory 1, offset 2^32
		0x1a,       // drop
		0x23, 0x00, // global.get 0
		0x1a, 0x0b,
	}
	m := &wasm.Module{
		Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}, {Limits: wasm.Limits{Min: 1, Addr64: true}}},
		Globals:  []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}},
		Code:     []wasm.Func{{BodyBytes: body}},
	}
	scores, err := computeModuleGlobalScores(m, 1)
	if err != nil || len(scores) != 1 || scores[0] != 1 {
		t.Fatalf("mixed-width global scores = %v, %v; want [1]", scores, err)
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
		if !h.flags.has(hintHasControlFlow) || !facts.hasControlFlow {
			t.Fatalf("opcode %#x control classification: production=%v inline=%v", op, h.flags.has(hintHasControlFlow), facts.hasControlFlow)
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
	hints, _, _, err := computeModuleHints(m, m.GlobalCount(), m.ImportedFuncCount())
	if err != nil {
		t.Fatalf("exported-table hints: %v", err)
	}
	if immutableTable := computeImmutableTableHint(m, hints, currentCodegenPolicy()); immutableTable.local {
		t.Fatal("module specialized an externally mutable exported table")
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
