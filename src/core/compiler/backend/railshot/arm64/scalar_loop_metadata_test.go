//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func scalarExitHistoryModule(t testing.TB, loops int) *wasm.Module {
	body := []byte{0, 0x02, 0x40}
	for i := 0; i < loops; i++ {
		body = append(body, 0x20, 0, 0x0d, 0, 0x02, 0x40, 0x03, 0x40,
			0x20, 1, 0x20, 2, 0x4f, 0x0d, 1,
			0x20, 3, 0x20, 1, 0x6a, 0x21, 3,
			0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b)
	}
	body = append(body, 0x0b, 0x20, 3, 0x20, 1, 0x6a, 0x0b)
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	return modFuncs(t, funcDef{params, []wasm.ValType{wasm.I32}, body}, funcDef{params, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x0b}})
}

func TestScalarLoopExitHistoryBounded(t *testing.T) {
	m := scalarExitHistoryModule(t, 1024)
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"scalar-loop-unroll": enabled}}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		c, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if c.CodeImage != nil {
			defer c.CodeImage.Close()
		}
		for _, escape := range []uint64{0, 1} {
			got, err := runArm64WrapperWithOptions(t, m, opts, escape, 0, 7, 11)
			want := uint64(39)
			if escape != 0 {
				want = 11
			}
			if err != nil || got != want {
				t.Fatalf("enabled=%t escape=%d got=%d err=%v want=%d", enabled, escape, got, err, want)
			}
		}
		if enabled && diagnosticsEnabled && !profileEnabled {
			p := stats.Funcs[0].Peephole
			if p["scalar-pretest-unroll4"]+p["scalar-loop-unroll-pretest"] == 0 || p["scalar-pretest-unroll4"]+p["scalar-loop-unroll-pretest"] >= 256 || p["scalar-loop-metadata-limit"] == 0 {
				t.Fatalf("unbounded exit history: %v", p)
			}
		}
	}
}

// Many small loops under one outer block grow its pending exit inventory.
// Keep this scaling shape separate from single-loop body-size benchmarks.
func BenchmarkScalarLoopExitHistory(b *testing.B) {
	for _, loops := range []int{4000, 16000, 64000} {
		m := scalarExitHistoryModule(b, loops)
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("loops=%d/unroll=%t", loops, enabled), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					c, err := CompileModuleWith(m, CompileOptions{Workers: 1, Optimizations: map[string]bool{"scalar-loop-unroll": enabled}})
					if err != nil {
						b.Fatal(err)
					}
					if c.CodeImage != nil {
						c.CodeImage.Close()
					}
				}
			})
		}
	}
}
