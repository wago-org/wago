//go:build (linux || darwin) && arm64

package arm64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestModuleScratchUsesBoundedStackArenaHintArm64(t *testing.T) {
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, []byte{0x00, 0x41, 0x2a, 0x0b})
	hints, _, _, err := computeModuleHints(m, m.GlobalCount(), m.ImportedFuncCount())
	if err != nil {
		t.Fatalf("compute hints: %v", err)
	}

	wantCap := minStackArenaCap
	gotCap := moduleStackArenaCap(m, hints)
	if gotCap != wantCap {
		t.Fatalf("module stack arena cap = %d, want %d", gotCap, wantCap)
	}
	sc := newScratchWithStackCap(gotCap)
	if got := cap(sc.stack.chunks[0]); got != wantCap {
		t.Fatalf("scratch first chunk cap = %d, want %d", got, wantCap)
	}

}

func TestModuleControlFrameCapIsExactAndLazyArm64(t *testing.T) {
	m := &wasm.Module{Code: make([]wasm.Func, 2)}
	if got := moduleControlFrameCap(m, []funcHints{{}, {}}); got != 0 {
		t.Fatalf("straight-line control cap = %d, want lazy zero", got)
	}
	if got := moduleControlFrameCap(m, []funcHints{{maxControlDepth: 2}, {maxControlDepth: 4}}); got != 5 {
		t.Fatalf("nested control cap = %d, want 5", got)
	}
}

func TestModuleControlFrameCapFallsBackConservativelyArm64(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{}}}
	if got := moduleControlFrameCap(m, nil); got != 0 {
		t.Fatalf("incomplete hints cap = %d, want zero fallback", got)
	}
	if got := moduleControlFrameCap(m, []funcHints{{maxControlDepth: maxHintedControlFrames}}); got != 0 {
		t.Fatalf("deep control cap = %d, want zero fallback", got)
	}
}

func TestWorkerControlFrameCapBoundsModuleOutlierArm64(t *testing.T) {
	m := &wasm.Module{Code: make([]wasm.Func, 3)}
	if got := workerControlFrameCap(m, []funcHints{{maxControlDepth: 2}, {maxControlDepth: 3}, {maxControlDepth: 40}}); got != maxWorkerInitialControlFrames {
		t.Fatalf("worker control cap = %d, want %d", got, maxWorkerInitialControlFrames)
	}
	if got := workerControlFrameCap(m, []funcHints{{maxControlDepth: 2}, {maxControlDepth: 3}, {maxControlDepth: 4}}); got != 5 {
		t.Fatalf("ordinary worker control cap = %d, want 5", got)
	}
}

func TestModuleStackArenaCapFallsBackForMultiValueTypesArm64(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32, wasm.I64}}}}}},
		Code:  []wasm.Func{{BodyBytes: []byte{0x0b}}},
	}
	if got := moduleStackArenaCap(m, []funcHints{{}}); got != defaultStackArenaCap {
		t.Fatalf("multi-value stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
}

func TestModuleStackArenaCapUsesCheapBodyBoundArm64(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 64)}}}
	hints := []funcHints{{localCount: 12}}
	want := stackArenaCapForBody(64, 12)
	if got := moduleStackArenaCap(m, hints); got != want {
		t.Fatalf("medium-function cap = %d, want body bound %d", got, want)
	}
}

func TestModuleStackArenaCapFallsBackWhenBodyBoundReachesDefaultArm64(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, defaultStackArenaCap*4/3+1)}}}
	if got := moduleStackArenaCap(m, []funcHints{{}}); got != defaultStackArenaCap {
		t.Fatalf("large-function cap = %d, want default %d", got, defaultStackArenaCap)
	}
}

func TestWorkerStackArenaCapDoesNotMultiplyLargeBodyArm64(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 4096)}}}
	hints := []funcHints{{}}
	if got := workerStackArenaCap(m, hints, inlineTargetTable{}, false); got != defaultStackArenaCap {
		t.Fatalf("worker stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
}

func TestInlineTargetsKeepLegacyStackArenaCapArm64(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0x0b}}}}
	hints := []funcHints{{}}
	targets := inlineTargetTable{data: &inlineTargetData{slots: []uint32{1}, targets: []inlineTarget{{}}}}
	if got := serialStackArenaCap(m, hints, targets, false); got != defaultStackArenaCap {
		t.Fatalf("serial inline stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
	if got := workerStackArenaCap(m, hints, targets, false); got != defaultStackArenaCap {
		t.Fatalf("worker inline stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
}

func TestGCTypeSubtypingUsesExpandedStackLoweringArm64(t *testing.T) {
	if expandedStackLowering(CompileOptions{}) {
		t.Fatal("empty options reported expanded stack lowering")
	}
	if !expandedStackLowering(CompileOptions{GCTypeSubtypingRefTest: true}) {
		t.Fatal("GC subtype helper did not report expanded stack lowering")
	}
}

func TestExpandedLoweringKeepsLegacyStackArenaCapArm64(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 512)}}}
	hints := []funcHints{{}}
	if got := serialStackArenaCap(m, hints, inlineTargetTable{}, true); got != defaultStackArenaCap {
		t.Fatalf("serial expanded-lowering stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
	if got := workerStackArenaCap(m, hints, inlineTargetTable{}, true); got != defaultStackArenaCap {
		t.Fatalf("worker expanded-lowering stack arena cap = %d, want %d", got, defaultStackArenaCap)
	}
}

func TestFunctionResultTypesUseBoundedScratchArm64(t *testing.T) {
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

func TestModuleGlobalMembershipUsesBorrowedBoundedPinsArm64(t *testing.T) {
	const nGlobals = 4096
	f := fn{
		m:         &wasm.Module{Globals: make([]wasm.Global, nGlobals)},
		globalReg: make([]Reg, nGlobals),
	}
	f.initGlobalRegs(nGlobals)
	pins := []moduleGlobalPin{{global: 123, reg: moduleGlobalRegs[0]}}
	if allocs := testing.AllocsPerRun(100, func() {
		f.installModuleGlobals(pins)
	}); allocs != 0 {
		t.Fatalf("module-global membership allocations = %v, want 0", allocs)
	}
	if !f.isModuleGlobal(123) || f.isModuleGlobal(124) {
		t.Fatalf("module-global membership mismatch")
	}
	f.globalReg[123] |= globalRegDirty
	if got := globalRegValue(f.globalReg[123]); got != moduleGlobalRegs[0] || !globalRegIsDirty(f.globalReg[123]) {
		t.Fatalf("packed global register = %d/%v", got, globalRegIsDirty(f.globalReg[123]))
	}
	backing := &f.globalReg[0]
	f.globalReg = f.globalReg[:0]
	if allocs := testing.AllocsPerRun(100, func() { f.initGlobalRegs(nGlobals) }); allocs != 0 {
		t.Fatalf("global register scratch reuse allocations = %v, want 0", allocs)
	}
	if &f.globalReg[0] != backing || f.globalReg[123] != regNone {
		t.Fatal("global register scratch was not reused and cleared")
	}
}

func TestGPPinLimitReservesTransientLoweringRegistersArm64(t *testing.T) {
	if got, want := gpPinLimit(0), len(gpAlloc)-4; got != want {
		t.Fatalf("pin limit without module reservations = %d, want %d", got, want)
	}
	reserved := maskOf(X23, X24, X25, X27)
	if got, want := gpPinLimit(reserved), len(gpAlloc)-8; got != want {
		t.Fatalf("pin limit with four module registers = %d, want %d", got, want)
	}
}

func TestHintSizedModuleStackArenaExecArm64(t *testing.T) {
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, []byte{0x00, 0x41, 0x2a, 0x0b})
	if got := runArm64(t, m); got != 42 {
		t.Fatalf("hint-sized scratch result = %d, want 42", got)
	}
}

// Allocator-pressure regression net, ported from amd64/{reg_pressure,
// regalloc_memref_spill,brtable_regalloc,allocation}_test.go. The amd64 versions
// pin their assertions to x86 fixed-role registers (RAX/RDX/RCX); the arm64 file
// keeps the same module shapes as end-to-end CORRECTNESS oracles, since arm64's
// orthogonal register file reaches the pressure differently (it has ~2× the
// allocatable GPRs and no fixed div/shift registers). Compile-success at extreme
// deferred-tree depth is itself the regression: these shapes used to hard-fail
// with "no register available to spill".

// regHeavyShiftChainArm64 builds a one-function module (with linear memory, so a
// register is reserved for memBytes) whose body computes a deep left-spine of
// variable-count shifts inside a loop: acc = ((((p0 << c1) << c2) ...). Each shift
// pins a value and a count register; nesting `depth` of them drives register
// pressure and, past the deferred-tree cap, forces the chain to be broken into
// register-sized segments. Mirrors amd64's regHeavyShiftChain.
func regHeavyShiftChainArm64(t *testing.T, nParams, depth int) *wasm.Module {
	t.Helper()
	params := make([]wasm.ValType, nParams)
	for i := range params {
		params[i] = wasm.I32
	}
	acc := byte(nParams)                       // accumulator local index (after the params)
	body := []byte{0x01, 0x01, 0x7f}           // one run of one i32 local
	body = append(body, 0x20, 0x00, 0x21, acc) // acc = p0
	body = append(body, 0x03, 0x40)            // loop (void) — runs once, boosts local scores
	body = append(body, 0x20, acc)             // acc
	for c := 0; c < depth; c++ {
		body = append(body, 0x20, byte(1+c%(nParams-1)), 0x74) // local.get p ; i32.shl
	}
	body = append(body, 0x21, acc) // acc = spine
	body = append(body, 0x0b)      // end loop
	body = append(body, 0x20, acc, 0x0b)

	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	memType := append([]byte{0x00}, wasmtest.ULEB(1)...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// TestExecRegHeavyShiftChainArm64 is the register-pressure regression: a deep
// nested-shift tree must compile via the deferred-tree depth cap breaking it into
// register-sized segments instead of failing to link,
// and must still compute the right value. Depths past ~14 used to hard-fail with
// "no register available to spill". Covers amd64's one-attempt register-pressure
// and deep-tree-cap regressions.
func TestExecRegHeavyShiftChainArm64(t *testing.T) {
	const nParams = 8
	for _, depth := range []int{7, 15, 20, 40, 100} {
		m := regHeavyShiftChainArm64(t, nParams, depth)
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatalf("depth %d: compile: %v", depth, err)
		}
		if cm.CodeImage != nil {
			_ = cm.CodeImage.Close()
		}
		if stats.Compile.FunctionAttempts != 1 || stats.Funcs[0].FunctionAttempts != 1 {
			t.Fatalf("depth %d: function attempts module/function = %d/%d, want 1/1", depth, stats.Compile.FunctionAttempts, stats.Funcs[0].FunctionAttempts)
		}
		args := make([]uint64, nParams)
		args[0] = 5
		for i := 1; i < nParams; i++ {
			args[i] = 1 // every shift count is 1, so the result is 5 << depth (0 once depth ≥ 32)
		}
		want := uint32(5) << depth
		if got := uint32(runArm64u(t, m, args...)); got != want {
			t.Fatalf("depth %d: shift chain = %d, want %d", depth, got, want)
		}
	}
}

// TestMemRefSpillKeepsLoadArm64 is the deferred-load (stMemRef) eviction
// regression: a deferred integer load holds its effective address in a register
// with the load not yet emitted; when that register is reclaimed under pressure,
// spill() must materialize the load rather than storing the address and silently
// dropping it (which would later use the address as if it were the loaded value).
// This miscompiled AssemblyScript's Unicode casemap() on amd64. The body forces the
// load's address through a div-heavy chain so the address register is reclaimed.
// For x = 12: mem[8] = 200, addr = 96/12 = 8, load8(8) = 200, x/3 = 4, result = 800.
func TestMemRefSpillKeepsLoadArm64(t *testing.T) {
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,       // 0 local decls
		0x41, 0x08, // i32.const 8
		0x41, 0xc8, 0x01, // i32.const 200
		0x3a, 0x00, 0x00, // i32.store8 (mem[8] = 200)
		0x41, 0xe0, 0x00, // i32.const 96
		0x20, 0x00, // local.get 0 (x)
		0x6e,             // i32.div_u  -> 96/x
		0x2d, 0x00, 0x00, // i32.load8_u [96/x]  (deferred load owns the address reg)
		0x20, 0x00, // local.get 0 (x)
		0x41, 0x03, // i32.const 3
		0x6e, // i32.div_u  -> x/3 (drives the reclaim that spills the pending load)
		0x6c, // i32.mul   (loaded byte * (x/3))
		0x0b, // end
	})
	if got := runArm64(t, m, 12); got != 800 {
		t.Fatalf("deferred load spilled as its address instead of its value: got %d, want 800", got)
	}
}

// brTableComputedIndexArm64 builds a br_table whose dispatch index is produced by a
// div (i32.div_u), the shape that stresses jump-table register allocation: the
// index must survive the table-base load. Six nested blocks give the br_table 5
// labels + a default (≥ brTableJumpMin, so the jump-table form fires); each arm
// returns 1000+label. Mirrors amd64's brTableIndexInRAX.
func brTableComputedIndexArm64(t *testing.T) *wasm.Module {
	return brTableComputedLabelsArm64(t, []uint32{0, 1, 2, 3, 4}, 5)
}

func brTableComputedLabelsArm64(t testing.TB, labels []uint32, def uint32) *wasm.Module {
	t.Helper()
	params := []wasm.ValType{wasm.I32, wasm.I32}
	body := []byte{0x00} // no locals
	maxLabel := def
	for _, lbl := range labels {
		if lbl > maxLabel {
			maxLabel = lbl
		}
	}
	blockN := int(maxLabel) + 1
	for i := 0; i < blockN; i++ {
		body = append(body, 0x02, 0x40) // block (void)
	}
	body = append(body, 0x20, 0x00, 0x20, 0x01, 0x6e) // local.get 0; local.get 1; i32.div_u
	body = append(body, 0x0e)
	body = append(body, wasmtest.ULEB(uint32(len(labels)))...)
	for _, lbl := range labels {
		body = append(body, wasmtest.ULEB(lbl)...)
	}
	body = append(body, wasmtest.ULEB(def)...)
	for i := 0; i < blockN; i++ {
		body = append(body, 0x0b) // end block i
		body = append(body, 0x41) // i32.const
		body = append(body, wasmtest.SLEB32(int32(1000+i))...)
		body = append(body, 0x0f) // return
	}
	body = append(body, 0x0b) // end func

	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestExecBrTableCompactTargetIDsArm64(t *testing.T) {
	labels := []uint32{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3}
	m := brTableComputedLabelsArm64(t, labels, 4)
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats}); err != nil {
		t.Fatalf("compile size: %v", err)
	}
	if stats.Funcs[0].Peephole["br-table-compact"] == 0 {
		t.Fatalf("duplicate table did not use compact target IDs: %v", stats.Funcs[0].Peephole)
	}
	for _, c := range []struct{ index, want uint64 }{
		{0, 1000}, {2, 1000}, {3, 1001}, {8, 1002}, {11, 1003}, {12, 1004},
	} {
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: true}, c.index, 1)
		if err != nil {
			t.Fatalf("f(%d,1): %v", c.index, err)
		}
		if got != c.want {
			t.Fatalf("f(%d,1) = %d, want %d", c.index, got, c.want)
		}
	}
}

func TestExecBrTableCompactTargetIDsImmediateBoundaryArm64(t *testing.T) {
	for _, labelN := range []int{4093, 4095} {
		t.Run(fmt.Sprint(labelN), func(t *testing.T) {
			labels := make([]uint32, labelN)
			m := brTableComputedLabelsArm64(t, labels, 1)
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats}); err != nil {
				t.Fatalf("compile size: %v", err)
			}
			if stats.Funcs[0].Peephole["br-table-compact"] != 0 {
				t.Fatalf("%d-label table used compact IDs past the add-immediate boundary: %v", labelN, stats.Funcs[0].Peephole)
			}
			for _, c := range []struct{ index, want uint64 }{{0, 1000}, {uint64(labelN - 1), 1000}, {uint64(labelN), 1001}} {
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: true}, c.index, 1)
				if err != nil {
					t.Fatalf("f(%d,1): %v", c.index, err)
				}
				if got != c.want {
					t.Fatalf("f(%d,1) = %d, want %d", c.index, got, c.want)
				}
			}
		})
	}
}

// TestExecBrTableComputedIndexArm64 is the br_table jump-table register-allocation
// regression: the dispatch index (from a div) must survive the table-base load and
// dispatch to the correct arm. It also asserts the jump-table lowering actually
// fired (not an if-chain fallback).
func TestExecBrTableComputedIndexArm64(t *testing.T) {
	beforeFinalizer := nativeFinalizerEnabled
	beforeCompact := nativeCompactionEnabled
	nativeFinalizerEnabled = true
	nativeCompactionEnabled = true
	t.Cleanup(func() {
		nativeFinalizerEnabled = beforeFinalizer
		nativeCompactionEnabled = beforeCompact
	})

	m := brTableComputedIndexArm64(t)

	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if ms.Funcs[0].Peephole["br-table-jump"] == 0 {
		t.Fatalf("br_table did not use the jump-table form: %v", ms.Funcs[0].Peephole)
	}

	// index = a/b, clamped to the default (label 5) when >= 5; arm returns 1000+idx.
	for _, c := range []struct{ a, b uint64 }{
		{0, 1}, {1, 1}, {4, 1}, {5, 1}, {9, 1}, {8, 4}, {100, 1},
	} {
		idx := c.a / c.b
		want := uint64(1000) + idx
		if idx >= 5 {
			want = 1005
		}
		if got := runArm64u(t, m, c.a, c.b); got != want {
			t.Fatalf("f(%d,%d): idx=%d got=%d want=%d", c.a, c.b, idx, got, want)
		}
	}
}

func TestExecBrTableCompactNativeUsesSmallerLinearFormArm64(t *testing.T) {
	m := brTableComputedIndexArm64(t)
	var balancedStats, sizeStats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &balancedStats}); err != nil {
		t.Fatalf("compile balanced: %v", err)
	}
	if _, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &sizeStats}); err != nil {
		t.Fatalf("compile size: %v", err)
	}
	if balancedStats.Funcs[0].Peephole["br-table-jump"] == 0 {
		t.Fatal("ordinary compilation did not retain jump-table dispatch")
	}
	if sizeStats.Funcs[0].Peephole["br-table-jump"] != 0 {
		t.Fatal("native compaction used a jump table for five unique cases")
	}
	if got, wantMax := sizeStats.NativeSize.TotalBytes, balancedStats.NativeSize.TotalBytes-8; got > wantMax {
		t.Fatalf("size code = %d bytes, want at most balanced %d - 8 = %d", got, balancedStats.NativeSize.TotalBytes, wantMax)
	}
	for _, c := range []struct{ a, b, want uint64 }{
		{0, 1, 1000}, {4, 1, 1004}, {5, 1, 1005}, {100, 1, 1005},
	} {
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: true}, c.a, c.b)
		if err != nil {
			t.Fatalf("f(%d,%d): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Fatalf("f(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestStackArenaOverflowKeepsExistingPointersStableArm64 checks the operand-stack
// arena: growing past its first chunk must keep already-handed-out element
// pointers valid and linked. Ported from amd64's identically-named test.
func TestStackArenaOverflowKeepsExistingPointersStableArm64(t *testing.T) {
	s := newStack()
	first := s.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	for i := 0; i < defaultStackArenaCap+8; i++ {
		s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i + 2)})
	}
	if first.elemKind() != ekValue || first.st.cval != 1 {
		t.Fatalf("first arena elem changed after overflow: kind=%v cval=%d", first.elemKind(), first.st.cval)
	}
	if s.node(s.head.next) != first {
		t.Fatal("first elem is no longer linked after arena overflow")
	}
	if len(s.chunks) < 2 {
		t.Fatalf("arena did not grow past its first chunk: %d", len(s.chunks))
	}
}
