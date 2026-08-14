//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestEntryOverwrittenParameterSkipsHomeARM64(t *testing.T) {
	for _, tc := range []struct {
		name      string
		readFirst bool
		wantElide int
	}{
		{name: "overwritten", wantElide: 1},
		{name: "read-near-miss", readFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := entryOverwrittenParamModuleARM64(t, tc.readFirst)
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
				t.Fatalf("compile: %v", err)
			}
			if got := stats.Funcs[0].Peephole["entry-param-home-elide"]; got != tc.wantElide {
				t.Fatalf("entry-param-home-elide = %d, want %d; peeps=%v", got, tc.wantElide, stats.Funcs[0].Peephole)
			}
			if got := runArm64u(t, m, make([]uint64, 16)...); got != 7 {
				t.Fatalf("result = %d, want 7", got)
			}
		})
	}
}

func entryOverwrittenParamModuleARM64(t testing.TB, readFirst bool) *wasm.Module {
	t.Helper()
	params := make([]wasm.ValType, 16)
	for i := range params {
		params[i] = wasm.I32
	}
	body := []byte{0x00}
	// Keep every other parameter hotter than parameter 15 so the bounded pin
	// allocator leaves the target in its frame slot on both target backends.
	for repeat := 0; repeat < 4; repeat++ {
		for i := byte(0); i < 15; i++ {
			body = append(body, 0x20, i, 0x1a) // local.get i; drop
		}
	}
	if readFirst {
		body = append(body, 0x20, 0x0f, 0x1a)
	}
	body = append(body,
		0x41, 0x07, 0x21, 0x0f, // i32.const 7; local.set 15
		0x20, 0x0f, // local.get 15
		0x0b)
	return mod1(t, params, []wasm.ValType{wasm.I32}, body)
}
