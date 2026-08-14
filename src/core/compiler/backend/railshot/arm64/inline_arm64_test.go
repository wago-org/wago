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
)

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

func TestInlineSizeObjectiveRequiresNativeByteProofArm64(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x05, 0x41, 0x07, 0x10, 0x01, 0x0b}
	leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
	)
	objective := OptimizeSize
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Stats: &stats})
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

func TestInlineSizeObjectivePrunesTransitiveOmissionArm64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x10, 0x02, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
	policy := shared.CodegenPolicyForObjective(currentCodegenPolicy().Selection, OptimizeSize)
	policy.MaxSizeInlineBodyBytes = 12
	hints := []funcHints{
		{hasCall: true},
		{nLocals: 1, hasCall: true, inlineCallSites: 1, directCallRefs: 1},
		{nLocals: 1, inlineCallSites: 1, directCallRefs: 1},
	}
	targets := buildInlineTargets(m, hints, policy)
	if targets.target(1) != nil {
		t.Fatal("transitive parent remained an inline target")
	}
	if targets.target(2) == nil || !targets.omitStandaloneBody(2, false) {
		t.Fatal("leaf child was not retained as an omittable inline target")
	}
}

func TestInlineSizeObjectiveAdmitsTinySingleUseLeafArm64(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}
	leaf := []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
	)
	before := inlineDeadBodyEnabled
	t.Cleanup(func() { inlineDeadBodyEnabled = before })
	objective := OptimizeSize
	compile := func(omit bool) (*a64.CompiledModule, *ModuleStats) {
		inlineDeadBodyEnabled = omit
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Stats: &stats, Workers: 1})
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
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got := arm64spike.Call2(uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]])), 0, 0); uint32(got) != 6 {
		t.Fatalf("inlined caller result = %d, want 6", got)
	}
	parallel, parallelStats := func() (*a64.CompiledModule, *ModuleStats) {
		var stats ModuleStats
		got, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Stats: &stats, Workers: 2})
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
	targets := inlineTargetTable{targets: []inlineTarget{{}, {valid: true, omitStandalone: true}}}
	err := finalizeOmittedInlineEntries(
		[]int{0, 12}, []int{4, 12},
		[][]callReloc{{{target: 1, internal: true}}, nil},
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
	objective := shared.CodegenPolicyForObjective(currentCodegenPolicy().Selection, OptimizeSize)
	base := []funcHints{{hasCall: true}, {nLocals: 1, inlineCallSites: 1, directCallRefs: 1}}
	if targets := buildInlineTargets(m, base, objective); !targets.omitStandaloneBody(1, false) {
		t.Fatal("single ordinary non-loop call did not prove standalone body dead")
	}
	tail := slices.Clone(base)
	tail[1].directCallRefs = 2
	if targets := buildInlineTargets(m, tail, objective); targets.omitStandaloneBody(1, false) {
		t.Fatal("tail-call reference permitted standalone body omission")
	}
	loop := slices.Clone(base)
	loop[1].hasInlineLoopCall = true
	if targets := buildInlineTargets(m, loop, objective); targets.omitStandaloneBody(1, false) {
		t.Fatal("ARM64 loop-site exclusion permitted standalone body omission")
	}
}

func TestInlineDeadBodyRetainsTailReferencedCalleeArm64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x12, 0x01, 0x0b}},
	)
	objective := OptimizeSize
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Stats: &stats, Workers: 1})
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
	objective := OptimizeSize
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Stats: &stats, Workers: 1})
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
