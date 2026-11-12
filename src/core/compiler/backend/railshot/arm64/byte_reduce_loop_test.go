//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestByteReduceLoopSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 2, 0x20, 1, 0x2d, 0, 0, 0x20, 1, 0x41, 1, 0x6a, 0x6c, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}
	for _, enabled := range []bool{false, true} {
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"byte-reduce-loop": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		want := 0
		if enabled {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["byte-reduce-loop"]; got != want {
			t.Fatalf("enabled=%t: selected=%d want=%d", enabled, got, want)
		}
	}
}

func TestByteReduceLoopAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 2, 0x20, 1, 0x2d, 0, 0, 0x20, 1, 0x41, 1, 0x6a, 0x6c, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}
	for _, tc := range []struct {
		name                               string
		mutate                             func([]byte)
		interrupt, shared, addr64, profile bool
	}{
		{name: "signed-limit", mutate: func(b []byte) { b[9] = 0x4e }},
		{name: "signed-byte", mutate: func(b []byte) { b[16] = 0x2c }},
		{name: "non-unit-step", mutate: func(b []byte) { b[31] = 2 }},
		{name: "changed-counter", mutate: func(b []byte) { b[29] = 0 }},
		{name: "interruptible", interrupt: true},
		{name: "shared", shared: true},
		{name: "memory64", addr64: true},
		{name: "source-maps", profile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.profile && !profileEnabled {
				t.Skip("source maps require wago_profile")
			}
			b := append([]byte(nil), body...)
			if tc.mutate != nil {
				tc.mutate(b)
			}
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
			m.Memories[0].Shared = tc.shared
			m.Memories[0].Limits.Addr64 = tc.addr64
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Interruptible: tc.interrupt, Profile: tc.profile, SourceMaps: tc.profile})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			want := 0
			if got := stats.Funcs[0].Peephole["byte-reduce-loop"]; got != want {
				t.Fatalf("selected=%d want=%d", got, want)
			}
		})
	}
}
