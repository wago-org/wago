//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestScalarPretestUnrollPreservesExits(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, ge := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x4f)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, add, ge = wasm.I64, 0x7e, 0x42, 0x7c, 0x5a
			fold = func(v uint64) uint64 { return v }
		}
		for _, variant := range []string{"ordinary", "outer-exit", "second-exit", "long-body", "memory", "interruptible", "compact"} {
			body := []byte{1, 1, bt, 0x02, 0x40}
			depth := byte(1)
			if variant == "outer-exit" {
				body = append(body, 0x02, 0x40)
				depth = 2
			}
			body = append(body, 0x03, 0x40, 0x20, 1, 0x20, 0, ge, 0x0d, depth)
			if variant == "second-exit" {
				eqz := byte(0x45)
				if wide {
					eqz = 0x50
				}
				body = append(body, 0x20, 2, eqz, 0x0d, depth)
			}
			repeats := uint64(1)
			if variant == "long-body" {
				repeats = 9
			}
			for range repeats {
				body = append(body, 0x20, 3, 0x20, 2, 0x20, 1, add, add, 0x21, 3)
			}
			if variant == "memory" {
				body = append(body, 0x41, 0, 0x20, 1)
				if wide {
					body = append(body, 0xa7)
				}
				body = append(body, 0x36, 2, 0)
			}
			body = append(body, 0x20, 1, k, 1, add, 0x21, 1, 0x0c, 0, 0x0b)
			if variant == "outer-exit" {
				body = append(body, 0x0b)
			}
			body = append(body, 0x0b, 0x20, 3, 0x20, 1, add, 0x0b)
			params, results := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}
			m := modFuncs(t, funcDef{params, results, body}, funcDef{params, results, []byte{0, 0x20, 2, 0x0b}})
			if variant == "memory" {
				m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			}
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for bound := uint64(0); bound < 14; bound++ {
						for _, initial := range []uint64{0, 2, ^uint64(0)} {
							for _, value := range []uint64{0, 3, 0x8000000000000001, ^uint64(0)} {
								index, sum := fold(initial), uint64(0)
								for index < bound && (variant != "second-exit" || fold(value) != 0) {
									sum = fold(sum + repeats*fold(value+index))
									index = fold(index + 1)
								}
								want := fold(sum + index)
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Interruptible: variant == "interruptible", CompactNative: variant == "compact", Optimizations: map[string]bool{"scalar-loop-unroll": enabled}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								nArg, iArg := bound, initial
								if !wide {
									nArg |= 0xdeadbeef00000000
									iArg |= 0xcafebabe00000000
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, nArg, iArg, value)
								if err != nil || fold(got) != want {
									t.Fatalf("wide=%t variant=%s enabled=%t workers=%d bound=%d initial=%x value=%x got=%x err=%v want=%x", wide, variant, enabled, workers, bound, initial, value, got, err, want)
								}
								positive := variant == "ordinary" || variant == "outer-exit"
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll-pretest"] != 0) != (enabled && positive && !profileEnabled) {
									t.Fatalf("path mismatch wide=%t variant=%s enabled=%t stats=%v", wide, variant, enabled, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}
