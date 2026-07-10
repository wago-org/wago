//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
