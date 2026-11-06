//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestStraightBlockIntegerParametersPreservePrefix(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, mul, add := wasm.I32, byte(0x6c), byte(0x6a)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, mul, add = wasm.I64, 0x7e, 0x7c
			fold = func(x uint64) uint64 { return x }
		}
		for _, nested := range []bool{false, true} {
			for _, mutate := range []bool{false, true} {
				body := []byte{0, 0x20, 0, 0x20, 0, 0x20, 1, 0x02, 1}
				if nested {
					body = append(body, 0x02, 1)
				}
				if mutate {
					body = append(body, 0x21, 0, 0x20, 0, add)
				} else {
					body = append(body, mul)
				}
				body = append(body, 0x0b)
				if nested {
					body = append(body, 0x0b)
				}
				body = append(body, add, 0x0b)
				params, results := []wasm.ValType{typ, typ}, []wasm.ValType{typ}
				m := modFuncs(t, funcDef{params, results, body}, funcDef{params, results, []byte{0, 0x20, 0, 0x0b}})
				for _, enabled := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, input := range [][2]uint64{{0, 0}, {3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
							a, b := fold(input[0]), fold(input[1])
							want := fold(a + fold(a*b))
							if mutate {
								want = fold(a + a + b)
							}
							if !wide {
								a |= 0xdeadbeef00000000
								b |= 0xcafebabe00000000
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"straight-line-block": enabled, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, _, err := runMemAmd64WithOptions(t, m, opts, nil, a, b)
							if err != nil || got != want {
								t.Fatalf("wide=%t nested=%t mutate=%t enabled=%t workers=%d input=%x got=%x err=%v want=%x", wide, nested, mutate, enabled, workers, input, got, err, want)
							}
							if diagnosticsEnabled {
								wantBlocks := 0
								if enabled {
									wantBlocks = 1
									if nested {
										wantBlocks = 2
									}
								}
								if gotBlocks := stats.Funcs[0].Peephole["straight-line-block"]; gotBlocks != wantBlocks {
									t.Fatalf("retained blocks=%d want=%d", gotBlocks, wantBlocks)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestStraightBlockNonIntegerParametersKeepFallback(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64, wasm.FuncRef, wasm.ExternRef, wasm.V128} {
		params, results := []wasm.ValType{typ}, []wasm.ValType{wasm.I32}
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x02, 1, 0x1a, 0x41, 7, 0x0b, 0x0b}}, funcDef{params, results, []byte{0, 0x41, 7, 0x0b}})
		var stats ModuleStats
		opts := CompileOptions{}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, _, err := runMemAmd64WithOptions(t, m, opts, nil, 0, 0)
		if err != nil || got != 7 {
			t.Fatalf("type=%v got=%d err=%v", typ, got, err)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["straight-line-block"] != 0 {
			t.Fatalf("type=%v bypassed typed fallback", typ)
		}
	}
}
