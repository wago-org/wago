//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCollectInlinedCalleesDeduplicatesPastStackSetArm64(t *testing.T) {
	const targetsN = inlineLinearSeenTargets + 1
	data := &inlineTargetData{slots: make([]uint32, targetsN), targets: make([]inlineTarget, targetsN)}
	body := make([]byte, 0, 2*(targetsN+1)+1)
	for i := range targetsN {
		data.slots[i] = uint32(i + 1)
		data.targets[i].globalIdx = i
		body = append(body, 0x10, byte(i))
	}
	body = append(body, 0x10, 0, 0x0b)
	targets := inlineTargetTable{data: data, classifier: wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)}
	got := collectInlinedCallees(&wasm.Func{BodyBytes: body}, targets)
	if len(got) != targetsN {
		t.Fatalf("distinct inline targets = %d, want %d", len(got), targetsN)
	}
	for i, target := range got {
		if target.globalIdx != i {
			t.Fatalf("inline target %d = %d, want %d", i, target.globalIdx, i)
		}
	}
}

func TestInlineBasePoolRetentionIsBoundedArm64(t *testing.T) {
	targetsData := &inlineTargetData{targets: make([]inlineTarget, maxRetainedInlineBases+1)}
	callees := make([]*inlineTarget, len(targetsData.targets))
	for i := range targetsData.targets {
		targetsData.targets[i].globalIdx = i
		callees[i] = &targetsData.targets[i]
	}
	targets := inlineTargetTable{data: targetsData}
	var f fn
	f.reserveInlineLocals(callees[:1], targets)
	if allocs := testing.AllocsPerRun(100, func() {
		f.reserveInlineLocals(callees[:1], targets)
	}); allocs != 0 {
		t.Fatalf("reused inline base allocations = %.0f, want 0", allocs)
	}
	f.reserveInlineLocals(callees, targets)
	if got := len(f.inlineBase); got != len(callees) {
		t.Fatalf("ephemeral inline bases = %d, want %d", got, len(callees))
	}
	if got := len(f.inlineBasePool); got != 1 {
		t.Fatalf("retained inline bases after oversized plan = %d, want 1", got)
	}
}

func TestInlineTargetSizeArm64(t *testing.T) {
	if got, want := unsafe.Sizeof(inlineTarget{}), uintptr(64); got != want {
		t.Fatalf("inlineTarget size = %d, want %d", got, want)
	}
}

func TestInlineTargetPlanWithoutCandidatesDoesNotAllocateArm64(t *testing.T) {
	m := modFuncs(t,
		funcDef{body: []byte{0x00, 0x10, 0x01, 0x0b}},
		funcDef{body: []byte{0x00, 0x10, 0x01, 0x0b}},
	)
	hints := []funcHints{{flags: hintHasCall}, {flags: hintHasCall}}
	policy := currentCodegenPolicy()
	var targets inlineTargetTable
	if allocs := testing.AllocsPerRun(100, func() {
		targets = buildInlineTargets(m, hints, policy)
	}); allocs != 0 {
		t.Fatalf("candidate-free inline plan allocations = %.0f, want 0", allocs)
	}
	if !targets.empty() {
		t.Fatal("candidate-free inline plan is not empty")
	}
}

func TestAnalyzeInlineCandidatesMixedMemory64MemargArm64(t *testing.T) {
	body := []byte{
		0x00,       // no locals
		0x42, 0x00, // i64.const 0
		0x28, 0x40, 0x01, 0x80, 0x80, 0x80, 0x80, 0x10, // i32.load memory 1, offset 1<<32
		0x1a, 0x0b, // drop; end
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}, []byte{0x04, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(module)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AnalyzeInlineCandidates(m); err != nil {
		t.Fatalf("analyze mixed-width module: %v", err)
	}
}

func TestInlineLeafExecAndStatsArm64(t *testing.T) {
	caller := []byte{
		0x00,
		0x41, 0x01, 0x41, 0x02, 0x10, 0x01,
		0x41, 0x03, 0x41, 0x04, 0x10, 0x01,
		0x6a, 0x0b,
	}
	leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
	)

	saved := inlineEnabled
	defer func() { inlineEnabled = saved }()
	for _, on := range []bool{false, true} {
		inlineEnabled = on
		if got := uint32(runArm64Internal2(t, m, 0, 0)); got != 10 {
			t.Fatalf("inline=%v result=%d, want 10", on, got)
		}
	}

	inlineEnabled = true
	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
		t.Fatalf("stats-only compile: %v", err)
	}
	if ms.Inline != nil {
		t.Fatalf("stats-only inline report = %#v, want nil", ms.Inline)
	}
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms, CollectInlineReport: true}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if ms.Inline == nil || ms.Inline.NumCandidates != 1 {
		t.Fatalf("inline report = %#v, want one candidate", ms.Inline)
	}
	if got := ms.Funcs[0].Calls["inline"]; got != 2 {
		t.Fatalf("Calls[inline] = %d, want 2", got)
	}
	if ms.Funcs[0].InlineSiteBytes == 0 {
		t.Fatal("inlined leaves have zero attributed inline-site bytes")
	}
}

func TestInlineBrOnNullRespectsCalleeBoundaryArm64(t *testing.T) {
	saved := inlineEnabled
	inlineEnabled = true
	defer func() { inlineEnabled = saved }()

	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x00, 0x10, 0x01, 0x1a, 0x41, 0x01, 0x0b}},
		funcDef{body: []byte{0x00, 0xd0, 0x70, 0xd5, 0x00, 0x1a, 0x0b}},
	)
	hints, _, _, err := computeModuleHints(m, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	target := buildInlineTargets(m, hints, currentCodegenPolicy()).target(1)
	if target == nil || !target.hasCtrl {
		t.Fatalf("inline target = %#v, want synthetic control boundary", target)
	}
	if got := uint32(runArm64Internal2(t, m, 0, 0)); got != 1 {
		t.Fatalf("caller result = %d, want 1", got)
	}
}

func TestInlineTargetsRejectEHArm64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x10, 0x01, 0x41, 0x01, 0x0b}},
		funcDef{body: []byte{0x00, 0x0b}},
	)
	policy := shared.DefaultCodegenPolicy(currentCodegenPolicy().Selection)
	hints := []funcHints{{flags: hintHasCall}, {flags: hintModuleEH, inlineCallSites: 1}}
	if target := buildInlineTargets(m, hints, policy).target(1); target != nil {
		t.Fatal("ordinary policy admitted EH inline target")
	}
}

func TestInlineRejectsRecursiveArm64(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x01, 0x10, 0x01, 0x0b}
	recursive := []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: recursive},
	)
	rep, err := AnalyzeInlineCandidates(m)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Funcs[1].Candidate {
		t.Fatalf("recursive function incorrectly marked inline candidate")
	}
}

func TestCompactInlineRequiresNativeByteProofArm64(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x05, 0x41, 0x07, 0x10, 0x01, 0x0b}
	leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
	)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Calls["inline"]; got != 0 {
		t.Fatalf("compact Calls[inline] = %d, want 0 without native-byte proof", got)
	}
}

func TestCompactInlinePrunesTransitiveOmissionArm64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x10, 0x02, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
	policy := shared.CompactCodegenPolicy(currentCodegenPolicy().Selection)
	policy.MaxCompactInlineBodyBytes = 12
	hints := []funcHints{
		{flags: hintHasCall},
		{localCount: 1, flags: hintHasCall, inlineCallSites: 1, directCallRefs: 1},
		{localCount: 1, inlineCallSites: 1, directCallRefs: 1},
	}
	targets := buildInlineTargets(m, hints, policy)
	if targets.target(1) != nil {
		t.Fatal("transitive parent remained an inline target")
	}
	if targets.target(2) == nil || !targets.omitStandaloneBody(2, false) {
		t.Fatal("leaf child was not retained as an omittable inline target")
	}
	if got, want := len(targets.data.targets), 1; got != want {
		t.Fatalf("retained inline target records = %d, want %d", got, want)
	}
	if got, want := len(targets.data.slots), len(m.Code); got != want {
		t.Fatalf("inline target slots = %d, want %d", got, want)
	}
}

func TestCompactInlineRetainsNestedCallPlanningArm64(t *testing.T) {
	m := modFuncs(t,
		// Keep arg 0 live while the single-use helper returns its result.
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x05, 0x10, 0x01, 0x6a, 0x0b}},
		// This tiny straight-line helper makes a real nested call.
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x10, 0x02, 0x0b}},
		// A declared local keeps the nested callee out of the Size inline class.
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x01, 0x01, 0x7f, 0x20, 0x00, 0x41, 0x02, 0x6a, 0x0b}},
	)
	var stats ModuleStats
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1}, 40)
	if err != nil {
		t.Fatal(err)
	}
	if inlined := stats.Funcs[0].Calls["inline"]; inlined != 0 {
		t.Fatalf("outer caller inlined call-making helper: Calls[inline] = %d", inlined)
	}
	if uint32(got) != 47 {
		t.Fatalf("nested-call caller result = %d, want 47", got)
	}
}

func TestCompactInlineAdmitsTinySingleUseLeafArm64(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}
	leaf := []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
	)
	before := inlineDeadBodyEnabled
	t.Cleanup(func() { inlineDeadBodyEnabled = before })
	compile := func(omit bool) (*a64.CompiledModule, *ModuleStats) {
		inlineDeadBodyEnabled = omit
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		return cm, &stats
	}
	rollback, _ := compile(false)
	if rollback.CodeImage != nil {
		defer rollback.CodeImage.Close()
	}
	cm, stats := compile(true)
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Calls["inline"]; got != 1 {
		t.Fatalf("compact Calls[inline] = %d, want 1 for proved tiny single-use leaf", got)
	}
	if len(cm.Code) >= len(rollback.Code) {
		t.Fatalf("dead-body code = %d bytes, rollback = %d", len(cm.Code), len(rollback.Code))
	}
	if got := stats.Funcs[1].Peephole["inline-dead-body"]; got != 1 || stats.Funcs[1].CodeBytes != 0 {
		t.Fatalf("omitted callee stats = %+v", stats.Funcs[1])
	}
	if cm.Entry[1] != cm.InternalEntry[1] || cm.InternalEntry[1] != cm.InternalEntry[0] {
		t.Fatalf("omitted entry/internal = %v/%v", cm.Entry, cm.InternalEntry)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got := arm64spike.Call2(uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]])), 0, 0); uint32(got) != 6 {
		t.Fatalf("inlined caller result = %d, want 6", got)
	}
	parallel, parallelStats := func() (*a64.CompiledModule, *ModuleStats) {
		var stats ModuleStats
		got, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 2})
		if err != nil {
			t.Fatal(err)
		}
		return got, &stats
	}()
	if !bytes.Equal(parallel.Code, cm.Code) || !slices.Equal(parallel.Entry, cm.Entry) || !slices.Equal(parallel.InternalEntry, cm.InternalEntry) {
		t.Fatal("serial and parallel dead-body layouts differ")
	}
	if got := parallelStats.Funcs[1].Peephole["inline-dead-body"]; got != 1 {
		t.Fatalf("parallel inline-dead-body = %d, want 1", got)
	}

	m.Exports = append(m.Exports, wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}})
	addressable, addressableStats := compile(true)
	if addressable.CodeImage != nil {
		defer addressable.CodeImage.Close()
	}
	if addressableStats.Funcs[1].CodeBytes == 0 || addressableStats.Funcs[1].Peephole["inline-dead-body"] != 0 {
		t.Fatalf("addressable callee was omitted: %+v", addressableStats.Funcs[1])
	}
}

func TestFinalizeOmittedInlineEntriesRejectsResidualCallArm64(t *testing.T) {
	targets := inlineTargetTable{data: &inlineTargetData{
		slots: []uint32{0, 1}, targets: []inlineTarget{{globalIdx: 1, omitStandalone: true}},
	}}
	err := finalizeOmittedInlineEntries(
		[]int{0, 12}, []int{4, 12},
		testCallRelocTable(t, []callReloc{{target: 1, internal: true}}, nil),
		[]bool{true, false}, targets,
	)
	if err == nil {
		t.Fatal("residual relocation to omitted inline body was accepted")
	}
}

func TestInlineDeadBodyProofRejectsTailAndLoopReferencesArm64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
	policy := shared.CompactCodegenPolicy(currentCodegenPolicy().Selection)
	base := []funcHints{{flags: hintHasCall}, {localCount: 1, inlineCallSites: 1, directCallRefs: 1}}
	if targets := buildInlineTargets(m, base, policy); !targets.omitStandaloneBody(1, false) {
		t.Fatal("single ordinary non-loop call did not prove standalone body dead")
	}
	tail := slices.Clone(base)
	tail[1].directCallRefs = 2
	if targets := buildInlineTargets(m, tail, policy); targets.omitStandaloneBody(1, false) {
		t.Fatal("tail-call reference permitted standalone body omission")
	}
	loop := slices.Clone(base)
	loop[1].flags.set(hintHasInlineLoopCall)
	if targets := buildInlineTargets(m, loop, policy); targets.omitStandaloneBody(1, false) {
		t.Fatal("ARM64 loop-site exclusion permitted standalone body omission")
	}
}

func TestInlineDeadBodyRetainsTailReferencedCalleeArm64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x12, 0x01, 0x0b}},
	)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if stats.Funcs[1].CodeBytes == 0 || stats.Funcs[1].Peephole["inline-dead-body"] != 0 {
		t.Fatalf("tail-referenced callee was omitted: %+v", stats.Funcs[1])
	}
}

func TestInlineDeadBodyRetainsArm64LoopSiteCallee(t *testing.T) {
	caller := []byte{
		0x00,
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x41, 0x05, 0x10, 0x01, 0x1a, // call leaf and drop
		0x0c, 0x01, // break out of block
		0x0b, 0x0b,
		0x41, 0x00, 0x0b,
	}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if stats.Funcs[1].CodeBytes == 0 || stats.Funcs[1].Peephole["inline-dead-body"] != 0 {
		t.Fatalf("loop-site callee was omitted: %+v", stats.Funcs[1])
	}
}
