//go:build amd64

package amd64

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

var vI32 = wasm.I32 // shorthand for the hand-built test bodies below

func TestInlineTargetPlanWithoutCandidatesDoesNotAllocateAMD64(t *testing.T) {
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

func TestCollectInlinedCalleesDeduplicatesPastStackSetAMD64(t *testing.T) {
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

func TestInlineBasePoolRetentionIsBoundedAMD64(t *testing.T) {
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

// TestAnalyzeInlineCandidates builds a small module exercising each candidacy
// outcome: a tiny leaf (candidate, two call sites), a recursive function
// (non-leaf via a self call), an oversized leaf (too big), and the caller.
func TestAnalyzeInlineCandidatesMixedMemory64Memarg(t *testing.T) {
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

func TestAnalyzeInlineCandidates(t *testing.T) {
	// func 0 (caller, ()->i32): calls func 1 twice and func 2 once.
	//   i32.const 1; i32.const 2; call 1; drop
	//   i32.const 3; i32.const 4; call 1; drop
	//   i32.const 5; call 2; end
	caller := []byte{
		0x00, // no local decls
		0x41, 0x01, 0x41, 0x02, 0x10, 0x01, 0x1a,
		0x41, 0x03, 0x41, 0x04, 0x10, 0x01, 0x1a,
		0x41, 0x05, 0x10, 0x02,
		0x0b,
	}
	// func 1 (leaf, (i32,i32)->i32): local.get 0; local.get 1; i32.add; end
	leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
	// func 2 (recursive, (i32)->i32): local.get 0; call 2; end
	recursive := []byte{0x00, 0x20, 0x00, 0x10, 0x02, 0x0b}
	// func 3 (oversized leaf, ()->i32): many (i32.const 0; drop) exceeding the size
	// ceiling, then i32.const 0; end
	big := []byte{0x00}
	for i := 0; i < 70; i++ {
		big = append(big, 0x41, 0x00, 0x1a) // i32.const 0; drop
	}
	big = append(big, 0x41, 0x00, 0x0b) // i32.const 0; end

	m := modFuncs(t,
		funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
		funcDef{params: []wasm.ValType{vI32, vI32}, results: []wasm.ValType{vI32}, body: leaf},
		funcDef{params: []wasm.ValType{vI32}, results: []wasm.ValType{vI32}, body: recursive},
		funcDef{params: nil, results: []wasm.ValType{vI32}, body: big},
	)

	rep, err := AnalyzeInlineCandidates(m)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(rep.Funcs) != 4 {
		t.Fatalf("Funcs len = %d, want 4", len(rep.Funcs))
	}

	// func 1 (leaf) is the only candidate, with two call sites.
	leafInfo := rep.Funcs[1]
	if !leafInfo.Candidate {
		t.Errorf("func 1 (leaf) should be a candidate; reason=%q", leafInfo.Reason)
	}
	if leafInfo.CallSites != 2 {
		t.Errorf("func 1 CallSites = %d, want 2", leafInfo.CallSites)
	}
	if leafInfo.Params != 2 || leafInfo.Results != 1 {
		t.Errorf("func 1 sig = %d->%d, want 2->1", leafInfo.Params, leafInfo.Results)
	}

	// func 0 (caller) is non-leaf.
	if rep.Funcs[0].Candidate {
		t.Errorf("func 0 (caller) should not be a candidate")
	}
	// func 2 (recursive) is non-leaf (it calls itself), CallSites==1.
	if rep.Funcs[2].Candidate {
		t.Errorf("func 2 (recursive) should not be a candidate; reason=%q", rep.Funcs[2].Reason)
	}
	// Two call sites target func 2: the caller's `call 2` and its own self-call.
	if rep.Funcs[2].CallSites != 2 {
		t.Errorf("func 2 CallSites = %d, want 2", rep.Funcs[2].CallSites)
	}
	// func 3 (oversized leaf) is rejected for size.
	if rep.Funcs[3].Candidate {
		t.Errorf("func 3 (big) should not be a candidate; reason=%q", rep.Funcs[3].Reason)
	}
	if rep.Funcs[3].BodyBytes <= inlineMaxBytes {
		t.Errorf("func 3 BodyBytes = %d, expected > %d", rep.Funcs[3].BodyBytes, inlineMaxBytes)
	}

	if rep.NumCandidates != 1 {
		t.Errorf("NumCandidates = %d, want 1", rep.NumCandidates)
	}
	if rep.TotalInlinableCallSites != 2 {
		t.Errorf("TotalInlinableCallSites = %d, want 2", rep.TotalInlinableCallSites)
	}
	if s := rep.String(); s == "" {
		t.Error("report String() is empty")
	}
}

// TestInlineReportInModuleStats verifies ordinary stats avoid the report scan,
// while an explicitly requested report is populated and rendered in String().
func TestInlineReportInModuleStats(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x01, 0x41, 0x02, 0x10, 0x01, 0x0b} // i32.const1;i32.const2;call 1;end
	leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}         // (i32,i32)->i32 a+b
	m := modFuncs(t,
		funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
		funcDef{params: []wasm.ValType{vI32, vI32}, results: []wasm.ValType{vI32}, body: leaf},
	)
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
	if ms.Inline == nil {
		t.Fatal("ModuleStats.Inline not populated during compile")
	}
	if ms.Inline.NumCandidates != 1 {
		t.Errorf("NumCandidates = %d, want 1", ms.Inline.NumCandidates)
	}
	if s := ms.String(); !strings.Contains(s, "inline candidates:") {
		t.Errorf("ModuleStats.String() missing inline report:\n%s", s)
	}
}

// withInlineEnabled runs fn with the WAGO_INLINE transform force-enabled,
// restoring the flag afterward (the package reads it once from the env at init).
func withInlineEnabled(t *testing.T, fn func()) {
	t.Helper()
	prev := inlineEnabled
	inlineEnabled = true
	defer func() { inlineEnabled = prev }()
	fn()
}

func TestInlineEnvEnabledDefaultAndOptOut(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want bool
	}{
		{"", true},
		{"1", true},
		{"true", true},
		{"yes", true},
		{"0", false},
		{"false", false},
		{"off", false},
		{"no", false},
		{" OFF ", false},
	} {
		if got := envDefaultOn(tc.env); got != tc.want {
			t.Fatalf("envDefaultOn(%q) = %v, want %v", tc.env, got, tc.want)
		}
	}
}

// TestInlineExecAdd inlines a straight-line leaf `add(a,b)=a+b` at a single call
// site and checks the spliced result is correct and that it was actually spliced
// (not called).
func TestInlineExecAdd(t *testing.T) {
	withInlineEnabled(t, func() {
		// func 0 ()->i32: i32.const 5; i32.const 7; call 1; end  → add(5,7)
		caller := []byte{0x00, 0x41, 0x05, 0x41, 0x07, 0x10, 0x01, 0x0b}
		leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b} // (i32,i32)->i32 a+b
		m := modFuncs(t,
			funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
			funcDef{params: []wasm.ValType{vI32, vI32}, results: []wasm.ValType{vI32}, body: leaf},
		)
		if got := runAmd64(t, m); got != 12 {
			t.Errorf("inlined add(5,7) = %d, want 12", got)
		}
		// Verify func 0 spliced the call instead of emitting one.
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatalf("compile: %v", err)
		}
		if ms.Funcs[0].Calls["inline"] != 1 {
			t.Errorf("func 0 Calls[inline] = %d, want 1 (calls=%v)", ms.Funcs[0].Calls["inline"], ms.Funcs[0].Calls)
		}
		if ms.Funcs[0].InlineSiteBytes == 0 {
			t.Error("inlined add has zero attributed inline-site bytes")
		}
	})
}

func TestInlineBrOnNullRespectsCalleeBoundaryAMD64(t *testing.T) {
	withInlineEnabled(t, func() {
		m := modFuncs(t,
			funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x00, 0x10, 0x01, 0x1a, 0x41, 0x01, 0x0b}},
			funcDef{body: []byte{0x00, 0xd0, 0x70, 0xd5, 0x00, 0x1a, 0x0b}},
		)
		hints, _, _, err := computeModuleHints(m, 0, 0, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		target := buildInlineTargets(m, hints, currentCodegenPolicy()).target(1)
		if target == nil || !target.hasCtrl {
			t.Fatalf("inline target = %#v, want synthetic control boundary", target)
		}
		if got := uint32(runAmd64u(t, m)); got != 1 {
			t.Fatalf("caller result = %d, want 1", got)
		}
	})
}

func TestInlineTargetsRejectEHAMD64(t *testing.T) {
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

func TestInlineBoundaryParityAMD64(t *testing.T) {
	var astFacts inlineFacts
	scanInlineFactsAST([]wasm.Instruction{
		{Kind: wasm.InstrBrOnNull},
		{Kind: wasm.InstrBrOnCast},
		{Kind: wasm.InstrTryTable},
	}, &astFacts)
	if !astFacts.hasControlFlow || !astFacts.moduleEH {
		t.Fatalf("AST inline facts = %#v", astFacts)
	}
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

func TestCompactInlineRequiresNativeByteProofAMD64(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x05, 0x41, 0x07, 0x10, 0x01, 0x0b}
	leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
	m := modFuncs(t,
		funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
		funcDef{params: []wasm.ValType{vI32, vI32}, results: []wasm.ValType{vI32}, body: leaf},
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
		t.Fatalf("Size Calls[inline] = %d, want 0 without native-byte proof", got)
	}
}

func TestCompactInlinePrunesTransitiveOmissionAMD64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x10, 0x02, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
	policy := shared.CompactCodegenPolicy(currentCodegenPolicy().Selection)
	policy.MaxCompactInlineBodyBytes = 12
	hints := []funcHints{
		{flags: hintHasCall},
		{localCount: 1, flags: hintHasCall, inlineCallSites: 1},
		{localCount: 1, inlineCallSites: 1},
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

func TestCompactInlineRetainsNestedCallPlanningAMD64(t *testing.T) {
	m := modFuncs(t,
		// Keep arg 0 live while the single-use helper returns its result.
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x05, 0x10, 0x01, 0x6a, 0x0b}},
		// This tiny straight-line helper makes a real nested call.
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x10, 0x02, 0x0b}},
		// A declared local keeps the nested callee out of the Size inline class.
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x01, 0x01, 0x7f, 0x20, 0x00, 0x41, 0x02, 0x6a, 0x0b}},
	)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Calls["inline"]; got != 0 {
		t.Fatalf("outer caller inlined call-making helper: Calls[inline] = %d", got)
	}
	if got := runCompiledAmd64u(t, cm, 40); uint32(got) != 47 {
		t.Fatalf("nested-call caller result = %d, want 47", got)
	}
}

func TestCompactInlineAdmitsTinySingleUseLeafAMD64(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}
	leaf := []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}
	m := modFuncs(t,
		funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
		funcDef{params: []wasm.ValType{vI32}, results: []wasm.ValType{vI32}, body: leaf},
	)
	before := inlineDeadBodyEnabled
	t.Cleanup(func() { inlineDeadBodyEnabled = before })
	inlineDeadBodyEnabled = false
	var rollbackStats ModuleStats
	rollback, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &rollbackStats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if rollback.CodeImage != nil {
		defer rollback.CodeImage.Close()
	}
	inlineDeadBodyEnabled = true
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Calls["inline"]; got != 1 {
		t.Fatalf("Size Calls[inline] = %d, want 1 for proved tiny single-use leaf", got)
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
	if got := runCompiledAmd64u(t, cm); uint32(got) != 6 {
		t.Fatalf("inlined caller result = %d, want 6", got)
	}
	var parallelStats ModuleStats
	parallel, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &parallelStats, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parallel.Code, cm.Code) || !reflect.DeepEqual(parallel.Entry, cm.Entry) || !reflect.DeepEqual(parallel.InternalEntry, cm.InternalEntry) {
		t.Fatal("serial and parallel dead-body layouts differ")
	}
	if got := parallelStats.Funcs[1].Peephole["inline-dead-body"]; got != 1 {
		t.Fatalf("parallel inline-dead-body = %d, want 1", got)
	}

	m.Exports = append(m.Exports, wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}})
	var addressableStats ModuleStats
	addressable, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &addressableStats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if addressable.CodeImage != nil {
		defer addressable.CodeImage.Close()
	}
	if addressableStats.Funcs[1].CodeBytes == 0 || addressableStats.Funcs[1].Peephole["inline-dead-body"] != 0 {
		t.Fatalf("addressable callee was omitted: %+v", addressableStats.Funcs[1])
	}
}

func TestFinalizeOmittedInlineEntriesRejectsResidualCallAMD64(t *testing.T) {
	targets := inlineTargetTable{data: &inlineTargetData{
		slots: []uint32{0, 1}, targets: []inlineTarget{{globalIdx: 1, omitStandalone: true}},
	}}
	err := finalizeOmittedInlineEntriesAMD64(
		[]int{0, 12}, []int{4, 12},
		[][]callReloc{{{target: 1, internal: true}}, nil},
		[]bool{true, false}, targets,
	)
	if err == nil {
		t.Fatal("residual relocation to omitted inline body was accepted")
	}
}

func TestInlineDeadBodyProofRejectsTailReferenceAMD64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
	policy := shared.CompactCodegenPolicy(currentCodegenPolicy().Selection)
	base := []funcHints{{flags: hintHasCall}, {localCount: 1, inlineCallSites: 1}}
	if targets := buildInlineTargets(m, base, policy); !targets.omitStandaloneBody(1, false) {
		t.Fatal("single ordinary call did not prove standalone body dead")
	}
	tail := append([]funcHints(nil), base...)
	tail[1].inlineCallSites |= 0x80
	if targets := buildInlineTargets(m, tail, policy); targets.omitStandaloneBody(1, false) {
		t.Fatal("tail-call reference permitted standalone body omission")
	}
}

func TestInlineDeadBodyRetainsTailReferencedCalleeAMD64(t *testing.T) {
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

// TestInlineExecTwoSites inlines the same callee at two sites in one caller,
// exercising the shared reserved-local region (rebound per site).
func TestInlineExecTwoSites(t *testing.T) {
	withInlineEnabled(t, func() {
		// func 0 ()->i32: add(1,2) + add(3,4) = 3 + 7 = 10
		caller := []byte{
			0x00,
			0x41, 0x01, 0x41, 0x02, 0x10, 0x01,
			0x41, 0x03, 0x41, 0x04, 0x10, 0x01,
			0x6a, 0x0b,
		}
		leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
		m := modFuncs(t,
			funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
			funcDef{params: []wasm.ValType{vI32, vI32}, results: []wasm.ValType{vI32}, body: leaf},
		)
		if got := runAmd64(t, m); got != 10 {
			t.Errorf("add(1,2)+add(3,4) = %d, want 10", got)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatalf("compile: %v", err)
		}
		if ms.Funcs[0].Calls["inline"] != 2 {
			t.Errorf("func 0 Calls[inline] = %d, want 2", ms.Funcs[0].Calls["inline"])
		}
	})
}

// TestInlineExecMemory inlines a leaf that does a memory store+load (Phase 3:
// memory-touching leaves are inlinable; the caller's guard-page pin exclusion is
// re-derived to include the spliced memory ops), verifying the memory path is
// correct through a splice.
func TestInlineExecMemory(t *testing.T) {
	withInlineEnabled(t, func() {
		// func 1 (addr,val)->i32 leaf: store val at addr, load it back.
		//   local.get 0; local.get 1; i32.store; local.get 0; i32.load; end
		leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}
		// func 0 ()->i32: put(16,42); drop; i32.load[16]  → 42
		caller := []byte{0x00, 0x41, 0x10, 0x41, 0x2a, 0x10, 0x01, 0x1a, 0x41, 0x10, 0x28, 0x02, 0x00, 0x0b}
		types := [][]byte{
			wasmtest.FuncType(nil, []wasm.ValType{vI32}),
			wasmtest.FuncType([]wasm.ValType{vI32, vI32}, []wasm.ValType{vI32}),
		}
		funcs := [][]byte{wasmtest.ULEB(0), wasmtest.ULEB(1)}
		codes := [][]byte{
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(leaf))), leaf...),
		}
		memType := append([]byte{0x00}, wasmtest.ULEB(1)...) // 1 page
		b := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(types...)),
			wasmtest.Section(3, wasmtest.Vec(funcs...)),
			wasmtest.Section(5, wasmtest.Vec(memType)),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(codes...)),
		)
		m, err := wasm.DecodeModule(b)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got := runAmd64(t, m); got != 42 {
			t.Errorf("inlined memory put/get = %d, want 42", got)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatalf("compile: %v", err)
		}
		if ms.Funcs[0].Calls["inline"] != 1 {
			t.Errorf("memory-touching leaf should be inlined; Calls=%v", ms.Funcs[0].Calls)
		}
	})
}

// TestInlineExecIfElse inlines a control-flow leaf `max(a,b)` (if/else), exercising
// the synthetic boundary frame + merge machinery.
func TestInlineExecIfElse(t *testing.T) {
	withInlineEnabled(t, func() {
		// func 1 (i32,i32)->i32: local.get0; local.get1; i32.gt_s; if(i32) local.get0 else local.get1 end; end
		leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x4a, 0x04, 0x7f, 0x20, 0x00, 0x05, 0x20, 0x01, 0x0b, 0x0b}
		// func 0 ()->i32: max(7,3) → 7
		caller := []byte{0x00, 0x41, 0x07, 0x41, 0x03, 0x10, 0x01, 0x0b}
		m := modFuncs(t,
			funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
			funcDef{params: []wasm.ValType{vI32, vI32}, results: []wasm.ValType{vI32}, body: leaf},
		)
		if got := runAmd64(t, m); got != 7 {
			t.Errorf("inlined max(7,3) = %d, want 7", got)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatalf("compile: %v", err)
		}
		if ms.Funcs[0].Calls["inline"] != 1 {
			t.Errorf("control-flow leaf should be inlined; Calls=%v", ms.Funcs[0].Calls)
		}
	})
}

// TestInlineExecIfElseReturn inlines a control-flow leaf that uses an early
// `return` inside an if, exercising opReturn's routing to the synthetic frame.
func TestInlineExecIfElseReturn(t *testing.T) {
	withInlineEnabled(t, func() {
		// func 1 (i32)->i32: if arg0 != 0 { return 100 }; 200
		//   local.get0; if(void) i32.const 100; return end; i32.const 200; end
		//   (i32.const 100 is 0x41 0xe4 0x00 in signed LEB128 — 0x64 alone is -28)
		leaf := []byte{0x00, 0x20, 0x00, 0x04, 0x40, 0x41, 0xe4, 0x00, 0x0f, 0x0b, 0x41, 0xc8, 0x01, 0x0b}
		// func 0 ()->i32: f(1) → 100
		caller := []byte{0x00, 0x41, 0x01, 0x10, 0x01, 0x0b}
		m := modFuncs(t,
			funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
			funcDef{params: []wasm.ValType{vI32}, results: []wasm.ValType{vI32}, body: leaf},
		)
		if got := runAmd64(t, m); got != 100 {
			t.Errorf("inlined early-return f(1) = %d, want 100", got)
		}
	})
}

// TestInlineExecReturnBare inlines `{ return arg0 }` — isolates opReturn routing.
func TestInlineExecReturnBare(t *testing.T) {
	withInlineEnabled(t, func() {
		leaf := []byte{0x00, 0x20, 0x00, 0x0f, 0x0b} // local.get0; return; end
		caller := []byte{0x00, 0x41, 0x2a, 0x10, 0x01, 0x0b}
		m := modFuncs(t,
			funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
			funcDef{params: []wasm.ValType{vI32}, results: []wasm.ValType{vI32}, body: leaf},
		)
		if got := runAmd64(t, m); got != 42 {
			t.Errorf("inlined {return arg0}(42) = %d, want 42", got)
		}
	})
}

// TestInlineExecNested feeds the result of one splice as an argument to another
// splice of the same callee (add(add(1,2), add(3,4))), exercising the
// result-decoupling in realizeInlineRange (the reserved region is rebound between
// the inner splices and the outer one).
func TestInlineExecNested(t *testing.T) {
	withInlineEnabled(t, func() {
		// func 0 ()->i32: add(1,2)=3; add(3,4)=7; add(3,7)=10
		caller := []byte{
			0x00,
			0x41, 0x01, 0x41, 0x02, 0x10, 0x01,
			0x41, 0x03, 0x41, 0x04, 0x10, 0x01,
			0x10, 0x01,
			0x0b,
		}
		leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
		m := modFuncs(t,
			funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
			funcDef{params: []wasm.ValType{vI32, vI32}, results: []wasm.ValType{vI32}, body: leaf},
		)
		if got := runAmd64(t, m); got != 10 {
			t.Errorf("add(add(1,2),add(3,4)) = %d, want 10", got)
		}
	})
}

// TestInlineExecDeclaredLocalZero inlines a callee that READS a declared local
// before writing it, checking the splice zero-initializes it (wasm semantics).
func TestInlineExecDeclaredLocalZero(t *testing.T) {
	withInlineEnabled(t, func() {
		// func 0 ()->i32: i32.const 9; call 1; end
		caller := []byte{0x00, 0x41, 0x09, 0x10, 0x01, 0x0b}
		// func 1 (i32)->i32 with 1 declared i32 local: local.get 0; local.get 1; i32.add; end
		//   returns a + t where t is the zero-initialized declared local → a.
		leaf := []byte{0x01, 0x01, 0x7f, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
		m := modFuncs(t,
			funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
			funcDef{params: []wasm.ValType{vI32}, results: []wasm.ValType{vI32}, body: leaf},
		)
		if got := runAmd64(t, m); got != 9 {
			t.Errorf("inlined f(9) with zero local = %d, want 9", got)
		}
	})
}

// TestAnalyzeInlineCandidatesUnused checks that a leaf with no call sites is
// reported as unused (not a candidate) rather than inlinable.
func TestAnalyzeInlineCandidatesUnused(t *testing.T) {
	// func 0 (exported, ()->i32): i32.const 7; end — never calls func 1.
	main := []byte{0x00, 0x41, 0x07, 0x0b}
	// func 1 (leaf, uncalled): i32.const 0; end
	unused := []byte{0x00, 0x41, 0x00, 0x0b}
	m := modFuncs(t,
		funcDef{params: nil, results: []wasm.ValType{vI32}, body: main},
		funcDef{params: nil, results: []wasm.ValType{vI32}, body: unused},
	)
	rep, err := AnalyzeInlineCandidates(m)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if rep.Funcs[1].Candidate {
		t.Errorf("uncalled leaf should not be a candidate; reason=%q", rep.Funcs[1].Reason)
	}
	if rep.Funcs[1].Reason != "no call sites" {
		t.Errorf("reason = %q, want %q", rep.Funcs[1].Reason, "no call sites")
	}
	if rep.NumCandidates != 0 {
		t.Errorf("NumCandidates = %d, want 0", rep.NumCandidates)
	}
}
