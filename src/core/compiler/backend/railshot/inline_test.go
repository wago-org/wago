package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var vI32 = wasm.I32 // shorthand for the hand-built test bodies below

// TestAnalyzeInlineCandidates builds a small module exercising each candidacy
// outcome: a tiny leaf (candidate, two call sites), a recursive function
// (non-leaf via a self call), an oversized leaf (too big), and the caller.
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
	// func 3 (oversized leaf, ()->i32): 20× (i32.const 0; drop) then i32.const 0; end
	big := []byte{0x00}
	for i := 0; i < 20; i++ {
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

// TestInlineReportInModuleStats verifies the report is populated on ModuleStats
// during a real compile and rendered in its String() (the WAGO_EXPLAIN path).
func TestInlineReportInModuleStats(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x01, 0x41, 0x02, 0x10, 0x01, 0x0b} // i32.const1;i32.const2;call 1;end
	leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}         // (i32,i32)->i32 a+b
	m := modFuncs(t,
		funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
		funcDef{params: []wasm.ValType{vI32, vI32}, results: []wasm.ValType{vI32}, body: leaf},
	)
	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
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
