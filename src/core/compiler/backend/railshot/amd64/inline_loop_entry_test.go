//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestInlineLoopEntryDefinitionsPreserveFreshReads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, add, sub := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6a), byte(0x6b)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, eqz, add, sub = wasm.I64, 0x7e, 0x42, 0x50, 0x7c, 0x7d
			fold = func(v uint64) uint64 { return v }
		}
		for _, earlyRead := range []bool{false, true} {
			helper := []byte{1, 3, bt, 0x20, 0}
			if earlyRead {
				helper = append(helper, 0x20, 1)
			} else {
				helper = append(helper, k, 17)
			}
			helper = append(helper, k, 1, 0x03, 2, 0x21, 2, 0x21, 1, 0x21, 0,
				0x20, 0, eqz, 0x04, 3, 0x20, 1, 0x20, 2, 0x05,
				0x20, 0, k, 1, sub, 0x20, 2, 0x20, 1, 0x20, 2, add, 0x0c, 1, 0x0b, 0x0b,
				0x20, 3, add, k, 9, 0x21, 3, 0x0b)
			caller := []byte{0, 0x20, 0, 0x10, 1, 0x20, 1, 0x10, 1, add, add, add, 0x0b}
			m := modFuncs(t,
				funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, caller},
				funcDef{[]wasm.ValType{typ}, []wasm.ValType{typ, typ}, helper},
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, []byte{0, 0x20, 1, 0x20, 2, 0x0b}},
				funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
			reference := func(count uint64) uint64 {
				a, b := uint64(17), uint64(1)
				if earlyRead {
					a = 0
				}
				for n := uint64(0); n < count; n++ {
					a, b = b, fold(a+b)
				}
				return fold(a + b)
			}
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, count := range []uint64{0, 1, 2, 3, 7, 21, 64, 79, 97} {
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						arg := count
						if !wide {
							arg |= 0xdeadbeef00000000
						}
						got, _, err := runMemAmd64WithOptions(t, m, opts, nil, arg, 3)
						want := fold(reference(count) + reference(3))
						if err != nil || got != want {
							t.Fatalf("wide=%t earlyRead=%t inline=%t workers=%d count=%d got=%x err=%v want=%x", wide, earlyRead, enabled, workers, count, got, err, want)
						}
						if diagnosticsEnabled {
							wantHits := 0
							if enabled && !profileEnabled {
								wantHits = 4
								if earlyRead {
									wantHits = 2
								}
							}
							if stats.Funcs[0].Peephole["inline-loop-entry-init"] != wantHits {
								t.Fatalf("wide=%t earlyRead=%t inline=%t profile=%t missing loop-entry proof: %v", wide, earlyRead, enabled, profileEnabled, stats.Funcs[0].Peephole)
							}
							if enabled && !profileEnabled && stats.Funcs[0].Peephole["inline-tuple-loop-local-pin"] < 4 {
								t.Fatal("fixture did not pin all private homes")
							}
						}
					}
				}
			}
		}
	}
}
