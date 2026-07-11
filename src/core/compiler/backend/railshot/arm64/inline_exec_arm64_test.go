//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// Inlining positive/negative suite, ported from amd64/inline_test.go. The existing
// inline_arm64_test.go covers a basic two-site leaf and recursion rejection; this
// adds the control-flow, memory, nested, declared-local-zero, loop-opt-in, and
// eligibility (size/unused) cases. Callers are run through runArm64u (a call-free
// entry after splicing), which also wires linear memory for the memory case.

func withInlineEnabledArm64(t *testing.T, fn func()) {
	t.Helper()
	saved := inlineEnabled
	inlineEnabled = true
	defer func() { inlineEnabled = saved }()
	fn()
}

func TestInlineExecMemoryArm64(t *testing.T) {
	withInlineEnabledArm64(t, func() {
		// leaf (addr,val)->i32: store val at addr, load it back.
		leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}
		// caller ()->i32: put(16,42); drop; i32.load[16] → 42
		caller := []byte{0x00, 0x41, 0x10, 0x41, 0x2a, 0x10, 0x01, 0x1a, 0x41, 0x10, 0x28, 0x02, 0x00, 0x0b}
		codes := [][]byte{
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(leaf))), leaf...),
		}
		b := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
			wasmtest.Section(5, wasmtest.Vec(append([]byte{0x00}, wasmtest.ULEB(1)...))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(codes...)),
		)
		m, err := wasm.DecodeModule(b)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got := uint32(runArm64u(t, m)); got != 42 {
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

func TestInlineExecIfElseArm64(t *testing.T) {
	withInlineEnabledArm64(t, func() {
		// leaf max(a,b): if a>b then a else b
		leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x4a, 0x04, 0x7f, 0x20, 0x00, 0x05, 0x20, 0x01, 0x0b, 0x0b}
		caller := []byte{0x00, 0x41, 0x07, 0x41, 0x03, 0x10, 0x01, 0x0b} // max(7,3)
		m := modFuncs(t,
			funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
			funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
		)
		if got := uint32(runArm64u(t, m)); got != 7 {
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

func TestInlineExecIfElseReturnArm64(t *testing.T) {
	withInlineEnabledArm64(t, func() {
		// leaf (i32)->i32: if arg0 != 0 { return 100 }; 200
		leaf := []byte{0x00, 0x20, 0x00, 0x04, 0x40, 0x41, 0xe4, 0x00, 0x0f, 0x0b, 0x41, 0xc8, 0x01, 0x0b}
		caller := []byte{0x00, 0x41, 0x01, 0x10, 0x01, 0x0b} // f(1) → 100
		m := modFuncs(t,
			funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
			funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
		)
		if got := uint32(runArm64u(t, m)); got != 100 {
			t.Errorf("inlined early-return f(1) = %d, want 100", got)
		}
	})
}

func TestInlineExecReturnBareArm64(t *testing.T) {
	withInlineEnabledArm64(t, func() {
		leaf := []byte{0x00, 0x20, 0x00, 0x0f, 0x0b} // local.get0; return; end
		caller := []byte{0x00, 0x41, 0x2a, 0x10, 0x01, 0x0b}
		m := modFuncs(t,
			funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
			funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
		)
		if got := uint32(runArm64u(t, m)); got != 42 {
			t.Errorf("inlined {return arg0}(42) = %d, want 42", got)
		}
	})
}

func TestInlineExecNestedArm64(t *testing.T) {
	withInlineEnabledArm64(t, func() {
		// add(add(1,2), add(3,4)) = 10
		caller := []byte{
			0x00,
			0x41, 0x01, 0x41, 0x02, 0x10, 0x01,
			0x41, 0x03, 0x41, 0x04, 0x10, 0x01,
			0x10, 0x01,
			0x0b,
		}
		leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
		m := modFuncs(t,
			funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
			funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
		)
		if got := uint32(runArm64u(t, m)); got != 10 {
			t.Errorf("add(add(1,2),add(3,4)) = %d, want 10", got)
		}
	})
}

func TestInlineExecDeclaredLocalZeroArm64(t *testing.T) {
	withInlineEnabledArm64(t, func() {
		caller := []byte{0x00, 0x41, 0x09, 0x10, 0x01, 0x0b} // f(9)
		// leaf (i32)->i32 with 1 declared i32 local: local.get0; local.get1; i32.add
		//   returns a + t where t is the zero-initialized declared local → a.
		leaf := []byte{0x01, 0x01, 0x7f, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
		m := modFuncs(t,
			funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
			funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
		)
		if got := uint32(runArm64u(t, m)); got != 9 {
			t.Errorf("inlined f(9) with zero local = %d, want 9", got)
		}
	})
}

func TestInlineExecLoopArm64(t *testing.T) {
	defer func(o bool) { inlineLoopCallees = o }(inlineLoopCallees)
	inlineLoopCallees = true // loop-carrying leaves are excluded by default
	withInlineEnabledArm64(t, func() {
		// leaf sum(n) = n+(n-1)+…+1, via a loop with an accumulator local.
		leaf := []byte{
			0x01, 0x01, 0x7f, // 1 local: acc
			0x03, 0x40, // loop (void)
			0x20, 0x01, 0x20, 0x00, 0x6a, 0x21, 0x01, // acc += n
			0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n -= 1
			0x20, 0x00, 0x41, 0x00, 0x4a, 0x0d, 0x00, // if n>0 br 0
			0x0b,       // end loop
			0x20, 0x01, // acc
			0x0b, // end func
		}
		caller := []byte{0x00, 0x41, 0x04, 0x10, 0x01, 0x0b} // sum(4) = 10
		m := modFuncs(t,
			funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
			funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
		)
		if got := uint32(runArm64u(t, m)); got != 10 {
			t.Errorf("inlined sum(4) = %d, want 10", got)
		}
	})
}

// TestInlineEligibilityArm64 covers the candidacy outcomes: a tiny leaf (candidate,
// two sites), a recursive function (non-leaf), an oversized leaf (too big), and the
// non-leaf caller.
func TestInlineEligibilityArm64(t *testing.T) {
	caller := []byte{
		0x00,
		0x41, 0x01, 0x41, 0x02, 0x10, 0x01, 0x1a,
		0x41, 0x03, 0x41, 0x04, 0x10, 0x01, 0x1a,
		0x41, 0x05, 0x10, 0x02,
		0x0b,
	}
	leaf := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
	recursive := []byte{0x00, 0x20, 0x00, 0x10, 0x02, 0x0b}
	big := []byte{0x00}
	for i := 0; i < 70; i++ {
		big = append(big, 0x41, 0x00, 0x1a)
	}
	big = append(big, 0x41, 0x00, 0x0b)
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: leaf},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: recursive},
		funcDef{results: []wasm.ValType{wasm.I32}, body: big},
	)
	rep, err := AnalyzeInlineCandidates(m)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !rep.Funcs[1].Candidate || rep.Funcs[1].CallSites != 2 {
		t.Errorf("func 1 (leaf) candidate=%v sites=%d, want true/2 (reason=%q)", rep.Funcs[1].Candidate, rep.Funcs[1].CallSites, rep.Funcs[1].Reason)
	}
	if rep.Funcs[0].Candidate {
		t.Errorf("func 0 (caller) should not be a candidate")
	}
	if rep.Funcs[2].Candidate {
		t.Errorf("func 2 (recursive) should not be a candidate; reason=%q", rep.Funcs[2].Reason)
	}
	if rep.Funcs[3].Candidate || rep.Funcs[3].BodyBytes <= inlineMaxBytes {
		t.Errorf("func 3 (oversized) candidate=%v bytes=%d (max %d)", rep.Funcs[3].Candidate, rep.Funcs[3].BodyBytes, inlineMaxBytes)
	}
	if rep.NumCandidates != 1 {
		t.Errorf("NumCandidates = %d, want 1", rep.NumCandidates)
	}
}

// TestInlineUnusedLeafArm64 checks a leaf with no call sites is reported unused.
func TestInlineUnusedLeafArm64(t *testing.T) {
	main := []byte{0x00, 0x41, 0x07, 0x0b}
	unused := []byte{0x00, 0x41, 0x00, 0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: main},
		funcDef{results: []wasm.ValType{wasm.I32}, body: unused},
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
