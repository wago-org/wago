//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLeafScratchMemorySizeSurvivesImmediateStore(t *testing.T) {
	body := []byte{1, 32, 0x7f, 0x41, 0, 0x21, 0}
	for x := byte(1); x < 32; x++ {
		body = append(body, 0x41, x, 0x21, x, 0x20, 0, 0x20, x, 0x6a, 0x21, 0)
	}
	body = append(body, 0x20, 0, 0x41)
	body = append(body, wasmtest.SLEB32(123)...)
	body = append(body, 0x36, 2, 0, 0x20, 0, 0x2d, 0, 0, 0x0b)
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		for _, guard := range []bool{false, true} {
			var stats ModuleStats
			opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"leaf-scratch-memsize": enabled, "regional-memory-read": false}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts)
			if err != nil || got != 123 {
				t.Fatalf("scratch=%v guard=%v got=%d err=%v", enabled, guard, got, err)
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["interval-region-scratch-memsize"] != 0 {
				t.Fatal("store can overwrite cached X17")
			}
		}
	}
}
