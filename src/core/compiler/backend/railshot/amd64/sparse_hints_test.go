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
