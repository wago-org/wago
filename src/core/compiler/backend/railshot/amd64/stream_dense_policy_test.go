//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func streamDensePolicyBody(locals, rotates, padding int, loop bool) []byte {
	body := []byte{1, byte(locals - 1), 0x7f}
	if loop {
		body = append(body, 0x03, 0x40)
	}
	for i := 0; i < rotates; i++ {
		n := byte(i % locals)
		body = append(body, 0x20, n, 0x41, 7, 0x77, 0x21, n)
	}
	for len(body) < padding {
		body = append(body, 0x01)
	}
	if loop {
		body = append(body, 0x0b)
	}
	return append(body, 0x20, 0, 0x0b)
}

func TestStreamDensePolicySelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	streamMemoryTestEnable(t)
	old := streamingPreserveDenseEnabled
	streamingPreserveDenseEnabled = true
	t.Cleanup(func() { streamingPreserveDenseEnabled = old })
	for _, tc := range []struct {
		name                                         string
		locals, rotates, padding                     int
		loop, interval, control, wide, bmi, preserve bool
	}{
		{"dense", 16, 128, 3300, false, true, true, true, true, true},
		{"interval-off", 16, 128, 3300, false, false, true, true, true, false},
		{"bmi-off", 16, 128, 3300, false, true, true, true, false, false},
		{"small-body", 16, 128, 0, false, true, true, true, true, false},
		{"few-rotates", 16, 127, 3300, false, true, true, true, true, false},
		{"few-locals", 15, 128, 3300, false, true, true, true, true, false},
		{"control-off", 16, 128, 3300, true, true, false, true, true, false},
		{"control-on", 16, 128, 3300, true, true, true, true, true, true},
		{"wide-loop", 80, 240, 3300, true, true, true, true, true, true},
		{"wide-pins-off", 80, 240, 3300, true, true, true, false, true, false},
		{"wide-straight", 80, 240, 3300, false, true, true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := streamDensePolicyBody(tc.locals, tc.rotates, tc.padding, tc.loop)
			m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, DeferCodeMapping: true, AMD64FeaturesSet: true, AMD64Features: shared.AMD64BMI2, Optimizations: map[string]bool{"bmi2-rorx": tc.bmi, "interval-region-pins": tc.interval, "interval-control": tc.control, "wide-local-pins": tc.wide}})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if len(stats.Funcs) != 1 {
				t.Fatal("missing function statistics")
			}
			p := stats.Funcs[0].Peephole
			got := p["stream-preserve-dense-regional"] != 0
			if got != tc.preserve {
				t.Fatalf("preserved=%v want=%v counters=%v", got, tc.preserve, p)
			}
			if tc.preserve && (p["streaming-function"] != 0 || p["dense-rotate-legacy"] == 0) {
				t.Fatalf("did not preserve actual legacy density policy: %v", p)
			}
			if !tc.preserve && !regallocCheckEnabled && p["streaming-function"] != 1 {
				t.Fatalf("negative silently fell back: %v", p)
			}
		})
	}
}

// Check the finalized plan, not merely the presence of a call opcode. This
// bounded fixture reuses the independently established selected-inline case.
func TestStreamDensePolicyInlinePrecedence(t *testing.T) {
	streamCallGlobalFlags(t)
	inlineEnabled = true
	oldDense, oldRorx := streamingPreserveDenseEnabled, streamingRorxEnabled
	streamingPreserveDenseEnabled, streamingRorxEnabled = true, true
	t.Cleanup(func() { streamingPreserveDenseEnabled, streamingRorxEnabled = oldDense, oldRorx })
	m := inlineCallFreeModule(t)
	var stats ModuleStats
	opts := CompileOptions{DeferCodeMapping: true}
	if diagnosticsEnabled {
		opts.Stats = &stats
	}
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := runCompiledAmd64u(t, cm, 40); got != 42 {
		t.Fatalf("inline result %d", got)
	}
	if diagnosticsEnabled && !regallocCheckEnabled {
		p := stats.Funcs[0].Peephole
		if p["all-calls-inlined"] == 0 {
			t.Fatalf("fixture did not select inline plan: %v", p)
		}
		if p["streaming-function"] != 0 || p["stream-preserve-dense-regional"] != 0 {
			t.Fatalf("selected inline plan was displaced: %v", p)
		}
	}
}
