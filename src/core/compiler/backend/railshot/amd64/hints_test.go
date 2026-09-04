//go:build linux && amd64

package amd64

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestImmediateFreeStackArenaHintParityAMD64(t *testing.T) {
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

func TestFuncHintsSize(t *testing.T) {
	const want = 32
	if got := unsafe.Sizeof(funcHints{}); got != want {
		t.Fatalf("funcHints size = %d, want %d", got, want)
	}
}

func TestParallelModuleHintsMatchSerial(t *testing.T) {
	for _, name := range []string{"json-as-simd.wasm", "lua.wasm", "sqlite3.wasm"} {
		t.Run(name, func(t *testing.T) {
			m := readParallelTestModule(t, "../../../../../../bench/corpus/"+name)
			policy := currentCodegenPolicy()
			serial, serialSidecar, serialGlobals, err := computeModuleHintsWithWorkersPolicy(m, m.GlobalCount(), m.ImportedFuncCount(), 1, nil, false, policy)
			if err != nil {
				t.Fatal(err)
			}
			parallel, parallelSidecar, parallelGlobals, err := computeModuleHintsWithWorkersPolicy(m, m.GlobalCount(), m.ImportedFuncCount(), 4, nil, false, policy)
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

func TestFuncHintPackedResolverAndRelocationCounts(t *testing.T) {
	var h funcHints
	for range 3 {
		h.addGCResolverSite()
	}
	for range 5 {
		h.addCallRelocSite()
	}
	if got := h.gcResolverSiteCount(); got != 3 {
		t.Fatalf("resolver sites = %d, want 3", got)
	}
	if got := h.callRelocSiteCount(); got != 5 {
		t.Fatalf("relocation sites = %d, want 5", got)
	}
	h.gcResolverAndRelocs = gcResolverSiteMask | uint32(^uint8(0))<<24
	h.addGCResolverSite()
	h.addCallRelocSite()
	if got := h.gcResolverSiteCount(); got != gcResolverSiteMask {
		t.Fatalf("saturated resolver sites = %d, want %d", got, gcResolverSiteMask)
	}
	if got := h.callRelocSiteCount(); got != ^uint8(0) {
		t.Fatalf("saturated relocation sites = %d, want %d", got, ^uint8(0))
	}
}

func TestEntryInitializedSharesLocalScoreStorage(t *testing.T) {
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

func TestScanBodyBytesDetectsDeepVariableShiftPressure(t *testing.T) {
	shiftChain := func(depth int, constantCount bool) []byte {
		body := []byte{0x20, 0x00} // local.get 0: accumulator
		for range depth {
			if constantCount {
				body = append(body, 0x41, 0x01) // i32.const 1
			} else {
				body = append(body, 0x20, 0x01) // local.get 1
			}
			body = append(body, 0x74) // i32.shl
		}
		return append(body, 0x0b)
	}
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{name: "below cap", body: shiftChain(maxDeferDepth-1, false)},
		{name: "at cap", body: shiftChain(maxDeferDepth, false), want: true},
		{name: "constant counts", body: shiftChain(maxDeferDepth+2, true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := scanBodyBytes(tc.body, 2, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := h.hasDeepVariableShift(); got != tc.want {
				t.Fatalf("deep variable shift = %v, want %v", got, tc.want)
			}
		})
	}

	var h funcHints
	h.addStackArenaDiscount(7)
	h.noteDeepVariableShift()
	h.addStackArenaDiscount(5)
	if got := h.arenaDiscount(); got != 12 || !h.hasDeepVariableShift() {
		t.Fatalf("packed discount/pressure = %d/%v, want 12/true", got, h.hasDeepVariableShift())
	}
}

func globalHint(h funcHintView, index uint32) (score uint32, eligible bool) {
	for _, hint := range h.sparseGlobals {
		if hint.Index == index {
			return hint.Score, hint.Eligible
		}
	}
	return 0, false
}

func TestConstantPreloadHints(t *testing.T) {
	body := []byte{0x43, 0, 0, 0x80, 0x3f, 0xfd, 12,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}
	h, err := scanBodyBytes(body, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !h.flags.has(hintHasFloatConst) || !h.flags.has(hintHasSIMD) {
		t.Fatalf("byte hints = float:%v SIMD:%v, want both", h.flags.has(hintHasFloatConst), h.flags.has(hintHasSIMD))
	}
	ast := scanBody(wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrF64Const}, {Kind: wasm.InstrV128Const},
	}}, 0, 0, 0)
	if !ast.flags.has(hintHasFloatConst) || !ast.flags.has(hintHasSIMD) {
		t.Fatalf("AST hints = float:%v SIMD:%v, want both", ast.flags.has(hintHasFloatConst), ast.flags.has(hintHasSIMD))
	}
	plain, err := scanBodyBytes([]byte{0x41, 0, 0x1a, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plain.flags.has(hintHasFloatConst) || plain.flags.has(hintHasSIMD) {
		t.Fatalf("integer hints = float:%v SIMD:%v, want neither", plain.flags.has(hintHasFloatConst), plain.flags.has(hintHasSIMD))
	}
}

func TestConstantPreloadHintsKeepMalformedStrict(t *testing.T) {
	for _, body := range [][]byte{{0x43}, {0xfd}, {0xfd, 12, 0}} {
		if _, err := scanBodyBytes(body, 0, 0, 0); err == nil {
			t.Fatalf("malformed body %x accepted", body)
		}
	}
}

func TestScanBodyHints(t *testing.T) {
	callOnly := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet},
		{Kind: wasm.InstrCall, Index: 7},
	}}
	h := scanBody(callOnly, 1, 0, 7)
	if !h.flags.has(hintHasCall) || h.flags.has(hintTouchesMemory) || !h.flags.has(hintCallsSelf) {
		t.Fatalf("call-only body: hasCall=%v touchesMemory=%v callsSelf=%v", h.flags.has(hintHasCall), h.flags.has(hintTouchesMemory), h.flags.has(hintCallsSelf))
	}
	if h.localScore[0] != 1 {
		t.Fatalf("local 0 score = %d, want 1", h.localScore[0])
	}
	if h2 := scanBody(callOnly, 1, 0, 8); h2.flags.has(hintCallsSelf) {
		t.Fatal("call to 7 should not count as self for index 8")
	}

	callMemory := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet},
		{Kind: wasm.InstrI32Load},
		{Kind: wasm.InstrCall},
		{Kind: wasm.InstrMemoryFill},
	}}
	h = scanBody(callMemory, 1, 0, 99)
	if !h.flags.has(hintHasCall) || !h.flags.has(hintTouchesMemory) || !h.flags.has(hintUsesBulkMem) {
		t.Fatalf("call+memory body: %+v", h)
	}
}

func TestScanBodyBytesCallHints(t *testing.T) {
	h, err := scanBodyBytes([]byte{0x10, 0x07, 0x0b}, 0, 0, 7) // call 7; end
	if err != nil {
		t.Fatalf("scan self call: %v", err)
	}
	if !h.flags.has(hintHasCall) || !h.flags.has(hintCallsSelf) {
		t.Fatalf("self call hints = %+v, want hasCall and callsSelf", h)
	}

	h, err = scanBodyBytes([]byte{0x11, 0x00, 0x00, 0x0b}, 0, 0, 0) // call_indirect type 0 table 0; end
	if err != nil {
		t.Fatalf("scan call_indirect: %v", err)
	}
	if !h.flags.has(hintHasCall) || h.flags.has(hintCallsSelf) {
		t.Fatalf("call_indirect hints = %+v, want hasCall without callsSelf", h)
	}
}

func TestBrTableJumpDataHintUsesBackendThreshold(t *testing.T) {
	for _, test := range []struct {
		name string
		n    byte
		want bool
	}{
		{"linear", brTableJumpMin - 1, false},
		{"jump table", brTableJumpMin, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte{0x0e, test.n}
			body = append(body, make([]byte, int(test.n)+1)...)
			body = append(body, 0x0b)
			h, err := scanBodyBytes(body, 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := h.flags.has(hintHasJumpTableData); got != test.want {
				t.Fatalf("jump-table-data hint = %v, want %v", got, test.want)
			}
		})
	}
}

func TestScanBodyExceptionHandlingHint(t *testing.T) {
	ast := scanBody(wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrThrow}}}, 0, 0, 0)
	if !ast.flags.has(hintModuleEH) {
		t.Fatal("AST throw did not mark exception handling")
	}
	bytes, err := scanBodyBytes([]byte{0x08, 0x00, 0x0b}, 0, 0, 0) // throw tag 0; end
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.flags.has(hintModuleEH) {
		t.Fatal("bytecode throw did not mark exception handling")
	}
	plain, err := scanBodyBytes([]byte{0x41, 0x00, 0x1a, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plain.flags.has(hintModuleEH) {
		t.Fatal("plain bytecode marked exception handling")
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
	if !storeHints.flags.has(hintHasStackSinkFusion) {
		t.Fatal("SIMD body did not select legacy arena sizing")
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

func TestScanBodyBytesStackArenaHintCountsAtomics(t *testing.T) {
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

func TestScanBodyBytesDiscountsAlgebraicIdentities(t *testing.T) {
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

func TestScanBodyBytesDetectsDeadCodeAfterTerminator(t *testing.T) {
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

func TestScanBodyBytesDiscountsSWARLookaheadCandidates(t *testing.T) {
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

func TestScanBodyBytesDetectsStackSinkFusion(t *testing.T) {
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

func TestScanBodyBytesStackArenaHintCountsReferenceResults(t *testing.T) {
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
	h, err := scanFuncBody(m.Code[0], nLocals, m.GlobalCount(), uint32(m.ImportedFuncCount()))
	if err != nil {
		t.Fatalf("scanFuncBody: %v", err)
	}
	legacy := stackArenaCapForBody(len(m.Code[0].BodyBytes), nLocals)
	hinted := stackArenaCapForHints(len(m.Code[0].BodyBytes), nLocals, int(h.stackArenaNodes))
	if h.stackArenaNodes == 0 || int(h.stackArenaNodes) >= len(m.Code[0].BodyBytes)/2 {
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
	if !h.flags.has(hintTouchesMemory) || h.flags.has(hintUsesBulkMem) {
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
	if !h.flags.has(hintTouchesMemory) || !h.flags.has(hintUsesBulkMem) {
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
	for index, wantScore := range map[uint32]uint32{1: 10, 2: 20} {
		if score, eligible := globalHint(h, index); score != wantScore || !eligible {
			t.Fatalf("global %d hint = (%d, %v), want (%d, true)", index, score, eligible, wantScore)
		}
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
	if score, eligible := globalHint(h, 0); score != 40 || !eligible { // two gets + one set, all at loop weight 10
		t.Fatalf("global hint = (%d, %v), want (40, true)", score, eligible)
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
	if !h.flags.has(hintHasCall) {
		t.Fatalf("hints = %+v, want hasCall", h)
	}
	if score, eligible := globalHint(h, 0); score == 0 || eligible {
		t.Fatalf("global hint = (%d, %v), want nonzero and ineligible", score, eligible)
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
				for _, hint := range h.sparseGlobals {
					want[hint.Index] += int64(hint.Score)
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
	want, _ := globalHint(scanBody(m.Code[0].Body, 0, 1, 0), 0)
	if len(got) != 1 || got[0] != int64(want) || got[0] != 30 {
		t.Fatalf("AST aggregate scores = %v, want %d", got, want)
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

func TestGCReferenceHintRequiresHelperAdmission(t *testing.T) {
	if gcOrAtomicInstructionMayCall(wasm.InstrRefTest, false) {
		t.Fatal("helper-free ref.test was classified as a call")
	}
	if !gcOrAtomicInstructionMayCall(wasm.InstrRefTest, true) {
		t.Fatal("generic ref.test helper call was not classified")
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

	allHints, sidecar, agg, err := computeModuleHints(m, m.GlobalCount(), 0, nil, false)
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
		ft, _ := m.LocalFuncType(i)
		wantLocals, err := countLocals(ft.Params, m.Code[i].Locals)
		if err != nil {
			t.Fatalf("countLocals %d: %v", i, err)
		}
		want, err := scanBodyBytes(m.Code[i].BodyBytes, wantLocals, m.GlobalCount(), uint32(i))
		if err != nil {
			t.Fatalf("scanBodyBytes %d: %v", i, err)
		}
		if !intervalRegionHintStorageEligible(true, len(m.Code[i].BodyBytes), want.nLocals, false) {
			want.localLastGet = nil
		}
		got := sidecar.view(allHints[i])
		got.localStart = 0
		got.lastGetStartPlus1 = 0
		got.globalStart = 0
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("func %d cached hints = %+v, want %+v", i, got, want)
		}
		if allHints[i].localCount != uint16(wantLocals) {
			t.Fatalf("func %d localCount = %d, want %d", i, allHints[i].localCount, wantLocals)
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
	if score, eligible := globalHint(h, hotGlobal); score != 30 || !eligible {
		t.Fatalf("hot global hint=(%d, %v), want (30, true)", score, eligible)
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

func TestGlobalScoreScannerConsumesMixedMemoryWidthImmediate(t *testing.T) {
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
	h, err := scanFuncBody(m.Code[0], 1, 3, 0)
	if err != nil {
		t.Fatalf("scan decoded body: %v", err)
	}
	if !h.flags.has(hintHasCall) || !h.flags.has(hintCallsSelf) {
		t.Fatalf("decoded recursive body hints = %+v, want call+self-call", h)
	}
	score0, _ := globalHint(h, 0)
	score1, eligible1 := globalHint(h, 1)
	score2, eligible2 := globalHint(h, 2)
	if h.localScore[0] == 0 || score0 == 0 || score1 == 0 || score2 == 0 {
		t.Fatalf("decoded byte-backed body produced missing scores: locals=%v globals=%v", h.localScore, h.sparseGlobals)
	}
	if !eligible1 {
		t.Fatalf("decoded loop without call should mark global 1 eligible: %v", h.sparseGlobals)
	}
	if eligible2 {
		t.Fatalf("decoded loop with self call should not mark global 2 eligible: %v", h.sparseGlobals)
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
	h, err := scanFuncBody(m.Code[0], 0, 0, 0)
	if err != nil {
		t.Fatalf("scan recursive body: %v", err)
	}
	if !h.flags.has(hintHasCall) || !h.flags.has(hintCallsSelf) {
		t.Fatalf("recursive decoded body hints = %+v, want hasCall and callsSelf", h)
	}
	if shouldSkipStackFence(h.flags.has(hintHasCall), 0, len(m.Code[0].BodyBytes)) {
		t.Fatalf("recursive call-making body was allowed to skip the stack fence")
	}
}

func TestScanBodyBytesMalformedImmediateReturnsError(t *testing.T) {
	if _, err := scanBodyBytes([]byte{0x10, 0x80, 0x0b}, 0, 0, 0); err == nil {
		t.Fatal("scan malformed call immediate succeeded, want error")
	}
}

func TestScanBodyBytesEntryInitializedLocals(t *testing.T) {
	// local 0 is read before its set and must retain Wasm's zero initialization.
	// Local 1's first access is a set in the straight-line entry prefix, so its
	// prologue zero is dead. Local 2 is first set inside a block and stays
	// conservative because that structured region can have alternate edges.
	body := []byte{
		0x20, 0x00, 0x1a, // local.get 0; drop
		0x41, 0x01, 0x21, 0x01, // i32.const 1; local.set 1
		0x02, 0x40, // block
		0x41, 0x02, 0x21, 0x02, // i32.const 2; local.set 2
		0x0b,                   // end block
		0x41, 0x03, 0x21, 0x00, // local 0's later set cannot undo its first read
		0x0b,
	}
	h, err := scanBodyBytes(body, 3, 0, 0)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got, want := h.entryInitialized, uint64(1)<<1; got != want {
		t.Fatalf("entryInitialized = %#x, want %#x", got, want)
	}
}

func TestControlDepthHintCountsNestedFrames(t *testing.T) {
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

func TestControlDepthHintSaturates(t *testing.T) {
	var h funcHints
	h.noteControlDepth(254)
	h.noteControlDepth(300)
	if h.maxControlDepth != 255 {
		t.Fatalf("saturated max control depth = %d, want 255", h.maxControlDepth)
	}
}

func TestControlDepthHintLeavesStraightLineLazy(t *testing.T) {
	h, err := scanBodyBytes([]byte{0x41, 0x00, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h.maxControlDepth != 0 {
		t.Fatalf("max control depth = %d, want 0", h.maxControlDepth)
	}
}
