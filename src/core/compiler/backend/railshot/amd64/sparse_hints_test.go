//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestComputeModuleHintsRetainsOnlyTouchedGlobals(t *testing.T) {
	const count = 128
	body := []byte{0x03, 0x40, 0x23, 0x7b, 0x1a, 0x0b, 0x0b} // loop { global.get 123; drop }
	m := sparseGlobalHintModule(count, body)
	hints, sidecar, aggregate, err := computeModuleHints(m, m.GlobalCount(), 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := aggregate[123]; got != count*10 {
		t.Fatalf("aggregate[123] = %d, want %d", got, count*10)
	}
	for i, summary := range hints {
		h := sidecar.view(summary)
		if len(h.sparseGlobals) != 1 || h.sparseGlobals[0].Index != 123 || h.sparseGlobals[0].Score != 10 || !h.sparseGlobals[0].Eligible {
			t.Fatalf("function %d sparse hints = %+v", i, h.sparseGlobals)
		}
	}
}

func TestCompileWideLocalWithCompactScores(t *testing.T) {
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, []byte{
		0x01, 0x64, 0x7f, // 100 i32 locals
		0x20, 0x63, 0x0b, // local.get 99; end
	})
	if _, err := CompileModule(m); err != nil {
		t.Fatal(err)
	}
	if got := runAmd64(t, m); got != 0 {
		t.Fatalf("local 99 = %d, want 0", got)
	}
}

func sparseGlobalHintModule(count int, body []byte) *wasm.Module {
	globals := make([]wasm.Global, count)
	for i := range globals {
		globals[i].Type = wasm.GlobalType{Type: wasm.I32, Mutable: true}
	}
	funcTypes := make([]wasm.TypeIdx, count)
	code := make([]wasm.Func, count)
	for i := range code {
		code[i].BodyBytes = body
	}
	return &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: funcTypes,
		Globals:   globals,
		Code:      code,
	}
}

func BenchmarkComputeModuleHintsSparseLocalHints(b *testing.B) {
	const (
		functions = 1024
		locals    = 256
	)
	m := &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: make([]wasm.TypeIdx, functions),
		Code:      make([]wasm.Func, functions),
	}
	for i := range m.Code {
		m.Code[i] = wasm.Func{
			Locals:    wasm.Locals{Runs: []wasm.LocalRun{{Count: locals, Type: wasm.I32}}},
			BodyBytes: []byte{0x0b},
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, _, err := computeModuleHints(m, 0, 0, nil, false); err != nil {
			b.Fatal(err)
		}
	}
}
