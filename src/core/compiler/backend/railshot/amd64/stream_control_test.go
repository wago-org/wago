//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

// Positive wired-entry gate. ON must reach the new whole-function engine, not
// merely pass via the legacy fallback. Runtime expectations are independent.
func TestStreamControlBasicResultIf(t *testing.T) {
	old := streamingOperandsEnabled
	defer func() { streamingOperandsEnabled = old }()
	body := []byte{0, 0x20, 0, 0x04, 0x7f, 0x20, 1, 0x41, 3, 0x6a, 0x05, 0x20, 2, 0x41, 4, 0x6b, 0x0b, 0x41, 2, 0x6c, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if !admitStreamFunction(&m.Code[0], &m.Types[0].SubTypes[0].Comp).Eligible {
		t.Fatal("result-if fixture not admitted")
	}
	for _, on := range []bool{false, true} {
		streamingOperandsEnabled = on
		for _, tc := range []struct{ cond, want uint64 }{{0, 32}, {1, 24}, {0xffffffff, 24}} {
			if got := runAmd64u(t, m, tc.cond, 9, 20); got != tc.want {
				t.Fatalf("enabled=%v cond=%x got=%d want=%d", on, tc.cond, got, tc.want)
			}
		}
		if diagnosticsEnabled {
			stats := compileWithStats(t, m, false)
			n := stats.Funcs[0].Peephole["streaming-function"]
			want := 0
			if on && !regallocCheckEnabled {
				want = 1
			}
			if n != want {
				t.Fatalf("enabled=%v streaming-function=%d want%d", on, n, want)
			}
		}
	}
}
