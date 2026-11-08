//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLinearFirstTermAndCompleteCancellation(t *testing.T) {
	for _, c := range []struct{ x, y int32 }{{1, 41}, {-1, 41}, {8, 41}, {60, 41}, {0, 41}, {0, 0}} {
		body := []byte{0, 0x03, 0x7f}
		coefficients := [6]int32{c.x - 14, c.y - 30, 7, 13, 7, 17}
		for i, k := range coefficients {
			body = append(body, 0x20, byte(i%2), 0x41)
			body = append(body, wasmtest.SLEB32(k)...)
			body = append(body, 0x6c)
			if i != 0 {
				body = append(body, 0x6a)
			}
		}
		body = append(body, 0x0b, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, enabled := range []bool{false, true} {
			for _, x := range []uint64{0, 1, 0x80000000, 0xdeadbeefffffffff} {
				for _, y := range []uint64{0, 7, 0xcafebabe80000001} {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"linear-integer-tree": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, x, y)
					want := uint64(uint32(x)*uint32(c.x) + uint32(y)*uint32(c.y))
					if err != nil || got != want {
						t.Fatalf("c=%+v enabled=%v x=%x y=%x: got=%x want=%x err=%v", c, enabled, x, y, got, want, err)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["linear-first-term"] != 0) != (enabled && (c.x != 0 || c.y != 0)) {
						t.Fatalf("c=%+v enabled=%v: selection %v", c, enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
