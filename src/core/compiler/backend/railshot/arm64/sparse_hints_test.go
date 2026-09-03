//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestComputeModuleHintsRetainsOnlyTouchedGlobals(t *testing.T) {
	const count = 128
	body := []byte{0x03, 0x40, 0x23, 0x7b, 0x1a, 0x0b, 0x0b} // loop { global.get 123; drop }
	globals := make([]wasm.Global, count)
	for i := range globals {
		globals[i].Type = wasm.GlobalType{Type: wasm.I32, Mutable: true}
	}
	funcTypes := make([]wasm.TypeIdx, count)
	code := make([]wasm.Func, count)
	for i := range code {
		code[i].BodyBytes = body
	}
	m := &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: funcTypes,
		Globals:   globals,
		Code:      code,
	}
	hints, aggregate, err := computeModuleHints(m, m.GlobalCount(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := aggregate[123]; got != count*10 {
		t.Fatalf("aggregate[123] = %d, want %d", got, count*10)
	}
	for i, h := range hints {
		if len(h.sparseGlobals) != 1 || h.sparseGlobals[0].Index != 123 || h.sparseGlobals[0].Score != 10 || !h.sparseGlobals[0].Eligible {
			t.Fatalf("function %d sparse hints = %+v", i, h.sparseGlobals)
		}
	}
}

func BenchmarkComputeModuleHintsSparseGlobalUse(b *testing.B) {
	const count = 1024
	body := []byte{0x03, 0x40, 0x23, 0x7b, 0x1a, 0x0b, 0x0b} // loop { global.get 123; drop }
	globals := make([]wasm.Global, count)
	for i := range globals {
		globals[i].Type = wasm.GlobalType{Type: wasm.I32, Mutable: true}
	}
	m := &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: make([]wasm.TypeIdx, count),
		Globals:   globals,
		Code:      make([]wasm.Func, count),
	}
	for i := range m.Code {
		m.Code[i].BodyBytes = body
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, _, err := computeModuleHints(m, m.GlobalCount(), 0); err != nil {
			b.Fatal(err)
		}
	}
}
