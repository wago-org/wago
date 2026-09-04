//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func sparseIntervalHintBody() []byte {
	body := make([]byte, minIntervalRegionBody+1)
	for i := range body[:len(body)-4] {
		body[i] = 0x01 // nop
	}
	copy(body[len(body)-4:], []byte{0x20, 0x00, 0x1a, 0x0b}) // local.get 0; drop; end
	return body
}

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
	hints, sidecar, aggregate, err := computeModuleHints(m, m.GlobalCount(), 0)
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

func TestComputeModuleHintsCompactsIntervalLastGets(t *testing.T) {
	saved := intervalRegionPinsEnabled
	intervalRegionPinsEnabled = true
	t.Cleanup(func() { intervalRegionPinsEnabled = saved })

	longBody := sparseIntervalHintBody()
	m := &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: make([]wasm.TypeIdx, 2),
		Code: []wasm.Func{
			{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: 64, Type: wasm.I32}}}, BodyBytes: []byte{0x0b}},
			{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: 32, Type: wasm.I32}}}, BodyBytes: longBody},
		},
	}
	hints, sidecar, _, err := computeModuleHints(m, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(sidecar.localScore), 96; got != want {
		t.Fatalf("local scores = %d, want %d", got, want)
	}
	if got, want := len(sidecar.localLastGet), 34; got != want {
		t.Fatalf("compact last gets = %d, want %d", got, want)
	}
	if got, want := int(sidecar.localLastGetRangeCount), 1; got != want {
		t.Fatalf("compact last-get ranges = %d, want %d", got, want)
	}
	if got := sidecar.view(hints[0]).localLastGet; got != nil {
		t.Fatalf("ineligible function retained last gets: %v", got)
	}
	eligible := sidecar.view(hints[1]).localLastGet
	if got, want := len(eligible), 32; got != want {
		t.Fatalf("eligible function last gets = %d, want %d", got, want)
	}
	if eligible[0] == 0 {
		t.Fatal("eligible function lost scanned local.get offset")
	}
}

func TestComputeModuleHintsKeepsCheaperDenseLastGets(t *testing.T) {
	saved := intervalRegionPinsEnabled
	intervalRegionPinsEnabled = true
	t.Cleanup(func() { intervalRegionPinsEnabled = saved })

	longBody := sparseIntervalHintBody()
	m := &wasm.Module{
		Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}},
		FuncTypes: make([]wasm.TypeIdx, 2),
		Code: []wasm.Func{
			{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: 32, Type: wasm.I32}}}, BodyBytes: longBody},
			{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: 32, Type: wasm.I32}}}, BodyBytes: longBody},
		},
	}
	hints, sidecar, _, err := computeModuleHints(m, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(sidecar.localLastGet), len(sidecar.localScore); got != want {
		t.Fatalf("dense last gets = %d, want %d", got, want)
	}
	if sidecar.localLastGetRangeCount != 0 {
		t.Fatalf("dense storage retained %d sparse ranges", sidecar.localLastGetRangeCount)
	}
	for i := range hints {
		if got, want := len(sidecar.view(hints[i]).localLastGet), 32; got != want {
			t.Fatalf("function %d last gets = %d, want %d", i, got, want)
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
	for i := 0; i < b.N; i++ {
		if _, _, _, err := computeModuleHints(m, m.GlobalCount(), 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComputeModuleHintsSparseIntervalLastGets(b *testing.B) {
	const (
		functions = 1024
		locals    = 64
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
		if _, _, _, err := computeModuleHints(m, 0, 0); err != nil {
			b.Fatal(err)
		}
	}
}
