package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
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
		if got := inlineEnvEnabled(tc.env); got != tc.want {
			t.Fatalf("inlineEnvEnabled(%q) = %v, want %v", tc.env, got, tc.want)
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
	})
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

// TestInlineExecLoop inlines a control-flow leaf with a loop: sum(n) = n+(n-1)+…+1.
func TestInlineExecLoop(t *testing.T) {
	withInlineEnabled(t, func() {
		// func 1 (n)->i32, locals: (acc i32). loop { acc += n; n -= 1; if n>0 continue }
		//   (local acc)
		//   loop
		//     local.get1; local.get0; i32.add; local.set1     ; acc += n
		//     local.get0; i32.const 1; i32.sub; local.set0     ; n -= 1
		//     local.get0; i32.const 0; i32.gt_s; br_if 0        ; if n>0 loop
		//   end
		//   local.get1                                          ; acc
		leaf := []byte{
			0x01, 0x01, 0x7f, // 1 local: acc i32
			0x03, 0x40, // loop (void)
			0x20, 0x01, 0x20, 0x00, 0x6a, 0x21, 0x01, // acc += n
			0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n -= 1
			0x20, 0x00, 0x41, 0x00, 0x4a, 0x0d, 0x00, // if n>0 br 0
			0x0b,       // end loop
			0x20, 0x01, // local.get acc
			0x0b, // end func
		}
		// func 0 ()->i32: sum(5) = 5+4+3+2+1 = 15
		caller := []byte{0x00, 0x41, 0x05, 0x10, 0x01, 0x0b}
		m := modFuncs(t,
			funcDef{params: nil, results: []wasm.ValType{vI32}, body: caller},
			funcDef{params: []wasm.ValType{vI32}, results: []wasm.ValType{vI32}, body: leaf},
		)
		if got := runAmd64(t, m); got != 15 {
			t.Errorf("inlined sum(5) = %d, want 15", got)
		}
		var ms ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
			t.Fatalf("compile: %v", err)
		}
		if ms.Funcs[0].Calls["inline"] != 1 {
			t.Errorf("loop leaf should be inlined; Calls=%v", ms.Funcs[0].Calls)
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
