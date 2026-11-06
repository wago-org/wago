//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScalarPretestLoopEveryExit(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			typ, k, add, ge := wasm.I32, byte(0x41), byte(0x6a), byte(0x4f)
			fold := func(x uint64) uint64 { return uint64(uint32(x)) }
			if wide {
				typ, k, add, ge = wasm.I64, 0x42, 0x7c, 0x5a
				fold = func(x uint64) uint64 { return x }
			}
			if signed {
				ge = 0x4e
				if wide {
					ge = 0x59
				}
			}
			body := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x20, 1, ge, 0x0d, 1,
				0x20, 2, 0x20, 0, add, 0x21, 2, 0x20, 0, k, 1, add, 0x21, 0,
				0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x20, 0, add, 0x0b}
			params := []wasm.ValType{typ, typ, typ}
			m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, body}, funcDef{params, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x0b}})
			inputs := [][2]uint64{{0, 0}, {7, 3}, {^uint64(0), 0}, {^uint64(0) - 1, ^uint64(0)}}
			for n := uint64(1); n <= 11; n++ {
				inputs = append(inputs, [2]uint64{0, n})
			}
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, input := range inputs {
						i, bound, sum := fold(input[0]), fold(input[1]), fold(^uint64(0)-7)
						for steps := 0; ; steps++ {
							less := i < bound
							if signed {
								less = int32(i) < int32(bound)
								if wide {
									less = int64(i) < int64(bound)
								}
							}
							if !less {
								break
							}
							if steps > 32 {
								t.Fatal("unbounded oracle input")
							}
							sum, i = fold(sum+i), fold(i+1)
						}
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, _, err := runMemAmd64WithOptions(t, m, opts, nil, input[0], input[1], ^uint64(0)-7)
						if err != nil || got != fold(sum+i) {
							t.Fatalf("wide=%t signed=%t on=%t workers=%d input=%x got=%x err=%v want=%x", wide, signed, enabled, workers, input, got, err, fold(sum+i))
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-pretest-unroll4"] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("on=%t profile=%t stats=%v", enabled, profileEnabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestScalarPretestLoopPreservesOlderCallerPrefix(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, add, ge := wasm.I32, byte(0x41), byte(0x6a), byte(0x4f)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, k, add, ge = wasm.I64, 0x42, 0x7c, 0x5a
			fold = func(x uint64) uint64 { return x }
		}
		// Capture the old sum beneath the loop. Mutating its source local must
		// not change that operand, including exits after any partial group.
		body := []byte{0, 0x20, 2, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x20, 1, ge, 0x0d, 1,
			0x20, 2, 0x20, 0, add, 0x21, 2, 0x20, 0, k, 1, add, 0x21, 0,
			0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x20, 0, add, add, 0x0b}
		params := []wasm.ValType{typ, typ, typ}
		m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, body}, funcDef{params, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for n := uint64(0); n < 13; n++ {
					for _, seed := range []uint64{17, ^uint64(0) - 5} {
						want := fold(seed)
						for i := uint64(0); i < n; i++ {
							want = fold(want + i)
						}
						want = fold(want + n + fold(seed))
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, _, err := runMemAmd64WithOptions(t, m, opts, nil, 0, n, seed)
						if err != nil || got != want {
							t.Fatalf("wide=%t on=%t workers=%d n=%d seed=%x got=%x err=%v want=%x", wide, enabled, workers, n, seed, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-pretest-unroll4"] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("on=%t profile=%t stats=%v", enabled, profileEnabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
