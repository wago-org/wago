//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestModuleStackArenaCapUsesSmallestBoundedHint(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0x00, 0x41, 0x2a, 0x0b}}}}
	hints := []funcHints{{stackArenaNodes: 1}}

	if got := moduleStackArenaCap(m, hints); got != minStackArenaCap {
		t.Fatalf("module stack arena cap = %d, want %d", got, minStackArenaCap)
	}
}

func TestModuleStackArenaCapFallsBackForIncompleteHints(t *testing.T) {
	m := &wasm.Module{Code: make([]wasm.Func, 2)}
	if got := moduleStackArenaCap(m, []funcHints{{}}); got != defaultStackArenaCap {
		t.Fatalf("incomplete-hint cap = %d, want legacy %d", got, defaultStackArenaCap)
	}
}

func TestModuleStackArenaCapFallsBackForMultiValueTypes(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32, wasm.I64}}}}}},
		Code:  []wasm.Func{{BodyBytes: []byte{0x0b}}},
	}
	if got := moduleStackArenaCap(m, []funcHints{{stackArenaNodes: 1}}); got != defaultStackArenaCap {
		t.Fatalf("multi-value stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
}

func TestModuleStackArenaCapFallsBackWhenLookaheadDiscountRemovesBenefit(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 1536)}}}
	hints := []funcHints{{stackArenaNodes: 770, stackArenaDiscount: 300}}
	if got := moduleStackArenaCap(m, hints); got != defaultStackArenaCap {
		t.Fatalf("discounted stack arena cap = %d, want legacy %d", got, defaultStackArenaCap)
	}
}

func TestModuleStackArenaCapFallsBackForDeadCode(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 1536)}}}
	hints := []funcHints{{stackArenaNodes: 770, hasStackSinkFusion: true}}
	if got := moduleStackArenaCap(m, hints); got != defaultStackArenaCap {
		t.Fatalf("dead-code stack arena cap = %d, want legacy %d", got, defaultStackArenaCap)
	}
}

func TestModuleStackArenaCapFallsBackForStackSinkFusion(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 1536)}}}
	hints := []funcHints{{stackArenaNodes: 770, hasStackSinkFusion: true}}
	if got := moduleStackArenaCap(m, hints); got != defaultStackArenaCap {
		t.Fatalf("stack-sink fusion cap = %d, want legacy %d", got, defaultStackArenaCap)
	}
}

func TestModuleStackArenaCapUsesBoundedLargeFunctionHint(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, defaultStackArenaCap*2)}}}
	hints := []funcHints{{stackArenaNodes: defaultStackArenaCap * 2}}
	want := stackArenaCapForHints(len(m.Code[0].BodyBytes), 0, int(hints[0].stackArenaNodes))
	if got := moduleStackArenaCap(m, hints); got != want {
		t.Fatalf("large-function cap = %d, want hinted %d", got, want)
	}

	m.Code[0].BodyBytes = make([]byte, maxInitialStackArenaCap*2)
	hints[0].stackArenaNodes = maxInitialStackArenaCap * 2
	if got := moduleStackArenaCap(m, hints); got != defaultStackArenaCap {
		t.Fatalf("pathological-function cap = %d, want fallback %d", got, defaultStackArenaCap)
	}
}

func TestModuleStackArenaCapFallsBackWhenHintExceedsLegacyRetention(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 1536)}}}
	hints := []funcHints{{stackArenaNodes: 768}}
	if hinted := stackArenaCapForHints(len(m.Code[0].BodyBytes), 0, int(hints[0].stackArenaNodes)); hinted != 1153 {
		t.Fatalf("test hinted cap = %d, want 1153", hinted)
	}
	if got := moduleStackArenaCap(m, hints); got != defaultStackArenaCap {
		t.Fatalf("over-reserved cap = %d, want legacy %d", got, defaultStackArenaCap)
	}
}

func TestWorkerStackArenaCapDoesNotMultiplyLargeHint(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 4096)}}}
	hints := []funcHints{{stackArenaNodes: 4096}}
	if got := workerStackArenaCap(m, hints, inlineTargetTable{}, false); got != defaultStackArenaCap {
		t.Fatalf("worker stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
}

func TestInlineTargetsKeepLegacyStackArenaCap(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0x0b}}}}
	hints := []funcHints{{stackArenaNodes: 1}}
	targets := inlineTargetTable{targets: []inlineTarget{{valid: true}}}
	if got := serialStackArenaCap(m, hints, targets, false); got != defaultStackArenaCap {
		t.Fatalf("serial inline stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
	if got := workerStackArenaCap(m, hints, targets, false); got != defaultStackArenaCap {
		t.Fatalf("worker inline stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
}

func TestGCTypeSubtypingUsesExpandedStackLowering(t *testing.T) {
	if expandedStackLowering(CompileOptions{}, CodegenPolicy{}) {
		t.Fatal("empty options reported expanded stack lowering")
	}
	if !expandedStackLowering(CompileOptions{GCTypeSubtypingRefTest: true}, CodegenPolicy{}) {
		t.Fatal("GC subtype helper did not report expanded stack lowering")
	}
	selection, err := optimizationBindings.ResolveSnapshot(map[string]bool{"gc-ref-facts": true}, OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !expandedStackLowering(CompileOptions{}, shared.DefaultCodegenPolicy(selection)) {
		t.Fatal("GC reference facts did not report expanded stack lowering")
	}
}

func TestExpandedLoweringKeepsLegacyStackArenaCap(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 512)}}}
	hints := []funcHints{{stackArenaNodes: 256}}
	if got := serialStackArenaCap(m, hints, inlineTargetTable{}, true); got != defaultStackArenaCap {
		t.Fatalf("serial expanded-lowering stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
	if got := workerStackArenaCap(m, hints, inlineTargetTable{}, true); got != defaultStackArenaCap {
		t.Fatalf("worker expanded-lowering stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
}

func TestModuleStackArenaCapIsDeterministicAcrossFunctionOrder(t *testing.T) {
	m1 := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 24)}, {BodyBytes: make([]byte, 80)}}}
	h1 := []funcHints{{stackArenaNodes: 8}, {stackArenaNodes: 20}}
	m2 := &wasm.Module{Code: []wasm.Func{m1.Code[1], m1.Code[0]}}
	h2 := []funcHints{h1[1], h1[0]}
	if got, want := moduleStackArenaCap(m1, h1), moduleStackArenaCap(m2, h2); got != want {
		t.Fatalf("cap depends on function order: %d != %d", got, want)
	}
}

func TestFunctionResultTypesUseBoundedScratch(t *testing.T) {
	var sc scratch
	got := lowerFunctionResultTypes(&sc, []wasm.ValType{wasm.I32, wasm.F64})
	if len(got) != 2 || got[0] != mtI32 || got[1] != mtF64 {
		t.Fatalf("lowered result types = %v, want [i32 f64]", got)
	}
	if &got[0] != &sc.functionResultTypeArena[0] {
		t.Fatal("common function results did not use scratch backing")
	}
	one := []wasm.ValType{wasm.I32}
	var sink machineType
	if allocs := testing.AllocsPerRun(100, func() {
		sink = lowerFunctionResultTypes(&sc, one)[0]
	}); allocs != 0 {
		t.Fatalf("common result lowering allocations = %v, want 0", allocs)
	}
	_ = sink

	wide := make([]wasm.ValType, maxScratchFunctionResults+1)
	overflow := lowerFunctionResultTypes(&sc, wide)
	if &overflow[0] == &sc.functionResultTypeArena[0] {
		t.Fatal("oversized function results unexpectedly used bounded scratch")
	}

	again := lowerFunctionResultTypes(&sc, []wasm.ValType{wasm.I64})
	if &again[0] != &sc.functionResultTypeArena[0] || again[0] != mtI64 {
		t.Fatalf("reused result scratch = %v, want [i64]", again)
	}
}

func TestHintSizedScratchRemovesFixedArenaBacking(t *testing.T) {
	saved := uintptr(defaultStackArenaCap-minStackArenaCap) * unsafe.Sizeof(elem{})
	if want := uintptr(24 << 10); saved < want {
		t.Fatalf("initial arena saving = %d bytes, want at least %d", saved, want)
	}
	sc := newScratchWithStackCap(minStackArenaCap)
	if got := cap(sc.stack.chunks[0]); got != minStackArenaCap {
		t.Fatalf("scratch first chunk cap = %d, want %d", got, minStackArenaCap)
	}
}

func TestModuleControlFrameCapIsExactAndLazy(t *testing.T) {
	m := &wasm.Module{Code: make([]wasm.Func, 2)}
	if got := moduleControlFrameCap(m, []funcHints{{}, {}}); got != 0 {
		t.Fatalf("straight-line control cap = %d, want lazy zero", got)
	}
	if got := moduleControlFrameCap(m, []funcHints{{maxControlDepth: 2}, {maxControlDepth: 4}}); got != 5 {
		t.Fatalf("nested control cap = %d, want 5", got)
	}
}

func TestModuleControlFrameCapFallsBackConservatively(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{}}}
	if got := moduleControlFrameCap(m, nil); got != 0 {
		t.Fatalf("incomplete hints cap = %d, want zero fallback", got)
	}
	if got := moduleControlFrameCap(m, []funcHints{{maxControlDepth: maxHintedControlFrames}}); got != 0 {
		t.Fatalf("deep control cap = %d, want zero fallback", got)
	}
}

func TestWorkerControlFrameCapBoundsModuleOutlier(t *testing.T) {
	m := &wasm.Module{Code: make([]wasm.Func, 3)}
	if got := workerControlFrameCap(m, []funcHints{{maxControlDepth: 2}, {maxControlDepth: 3}, {maxControlDepth: 40}}); got != maxWorkerInitialControlFrames {
		t.Fatalf("worker control cap = %d, want %d", got, maxWorkerInitialControlFrames)
	}
	if got := workerControlFrameCap(m, []funcHints{{maxControlDepth: 2}, {maxControlDepth: 3}, {maxControlDepth: 4}}); got != 5 {
		t.Fatalf("ordinary worker control cap = %d, want 5", got)
	}
}

func TestParallelControlFrameScratchDoesNotMultiplyOutlier(t *testing.T) {
	const workers, depth = 4, 40
	m := benchParallelControlOutlierModule(t, 64, depth)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Workers: workers, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	frameBytes := uint64(unsafe.Sizeof(ctrlFrame{}))
	if got, want := stats.Compile.ControlScratchReserved, uint64(workers*maxWorkerInitialControlFrames)*frameBytes; got != want {
		t.Fatalf("parallel control scratch reserved = %d, want %d", got, want)
	}
	oldReservation := uint64(workers*(depth+1)) * frameBytes
	if stats.Compile.ControlScratchPeak >= oldReservation {
		t.Fatalf("parallel control peak envelope = %d, want below former reservation %d", stats.Compile.ControlScratchPeak, oldReservation)
	}
	if stats.Compile.ControlScratchRetained != 0 || stats.Compile.ControlScratchDiscarded != stats.Compile.ControlScratchPeak {
		t.Fatalf("finished worker control scratch retained/discarded = %d/%d, want 0/%d",
			stats.Compile.ControlScratchRetained, stats.Compile.ControlScratchDiscarded, stats.Compile.ControlScratchPeak)
	}
	t.Logf("control scratch: reserved=%d peak-envelope=%d retained=%d former-reservation=%d",
		stats.Compile.ControlScratchReserved, stats.Compile.ControlScratchPeak,
		stats.Compile.ControlScratchRetained, oldReservation)
}

func TestAsmCapForBodyClamps(t *testing.T) {
	for _, tc := range []struct {
		bodyLen int
		wantMin int
		wantMax int
	}{
		{0, 128, 128},
		{8, 128, 128},
		{64, 320, 320},
		{1 << 20, 64 << 10, 64 << 10},
	} {
		got := asmCapForBody(tc.bodyLen)
		if got < tc.wantMin || got > tc.wantMax {
			t.Fatalf("asmCapForBody(%d) = %d, want in [%d,%d]", tc.bodyLen, got, tc.wantMin, tc.wantMax)
		}
	}
}

func TestInstructionCorpusCompiles(t *testing.T) {
	corpus := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{
		"isa_i32.wasm", "isa_i64.wasm", "isa_f32.wasm", "isa_f64.wasm",
		"isa_cvt.wasm", "isa_ctl.wasm", "isa_call.wasm", "isa_mem.wasm",
		"isa_bulk_mem.wasm", "isa_var.wasm",
		"isa_simd_i8x16.wasm", "isa_simd_i16x8.wasm", "isa_simd_i32x4.wasm",
		"isa_simd_i64x2.wasm", "isa_simd_f32x4.wasm", "isa_simd_f64x2.wasm",
		"isa_simd_reduce.wasm", "isa_simd_v128.wasm",
		"arith.wasm", "branches.wasm", "dispatch.wasm", "fib_iter.wasm", "fib_rec.wasm",
		"float.wasm", "globals.wasm", "linked_list.wasm", "many_funcs.wasm", "memory.wasm",
		"memory_tree.wasm", "sieve.wasm", "matmul.wasm", "quicksort.wasm", "fannkuch.wasm",
		"nbody.wasm", "sha256.wasm", "raytrace.wasm", "spectralnorm.wasm", "wasm3.wasm",
		"base64x.wasm", "bignum.wasm", "blake3sum.wasm", "json-as.wasm", "utf-as.wasm",
		"blake-as.wasm", "blake-as-simd.wasm", "json-as-simd.wasm", "utf-as-simd.wasm",
		"jsonproc.wasm", "lua.wasm", "markdown.wasm", "sqlite3.wasm", "regexmatch.wasm",
		"crc32.wasm", "crcsum.wasm", "script.wasm", "esbuild.wasm", "ruby.wasm",
	} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(corpus, name))
			if err != nil {
				t.Fatal(err)
			}
			mod, err := frontend.DecodeValidate(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CompileModule(mod); err != nil {
				t.Fatal(err)
			}
		})
	}
}
