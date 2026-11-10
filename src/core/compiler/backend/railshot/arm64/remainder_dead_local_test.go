//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRemainderPairOmitsUnreadLocalRestores(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, rem, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x70), byte(0x6a)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, eqz, rem, add = wasm.I64, 0x7e, 0x42, 0x50, 0x82, 0x7c
			fold = func(v uint64) uint64 { return v }
		}
		for _, visible := range []bool{false, true} {
			result := byte(0)
			if visible {
				result = 3
			}
			body := []byte{1, 2, bt, k, 0x7f, 0x21, 3, 0x02, 0x40, 0x03, 0x40, 0x20, 1, eqz, 0x0d, 1,
				0x20, 0, 0x20, 1, rem, 0x21, 3, 0x20, 1, 0x21, 0, 0x20, 3, 0x21, 1,
				0x20, 2, k, 1, add, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, result, 0x20, 2, add, 0x0b}
			m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, body})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, pair := range [][2]uint64{{0, 0}, {1, 0}, {0, 1}, {13, 8}, {21, 13}, {34, 21}, {55, 34}, {89, 55}, {^uint64(0), 17}, {0x8000000000000001, 29}} {
						a, b, r, steps := fold(pair[0]), fold(pair[1]), fold(^uint64(0)), uint64(0)
						for b != 0 {
							r = a % b
							a, b = b, r
							steps++
						}
						want := fold(a + steps)
						if visible {
							want = fold(r + steps)
						}
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, pair[0], pair[1])
						if err != nil || fold(got) != want {
							t.Fatalf("wide=%t visible=%t enabled=%t workers=%d pair=%x got=%x err=%v want=%x", wide, visible, enabled, workers, pair, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-remainder-dead-local"] != 0) != (enabled && !visible && !profileEnabled) {
							t.Fatalf("dead-local path mismatch: %v", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestRemainderPairRetainsLocalAcrossOuterBackedge(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, rem, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x70), byte(0x6a)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, eqz, rem, add = wasm.I64, 0x7e, 0x42, 0x50, 0x82, 0x7c
			fold = func(v uint64) uint64 { return v }
		}
		// R is read before the inner loop; the later outer branch revisits that read.
		body := []byte{1, 2, bt, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
			0x20, 5, 0x20, 3, add, 0x21, 5, 0x02, 0x40, 0x03, 0x40, 0x20, 2, eqz, 0x0d, 1,
			0x20, 1, 0x20, 2, rem, 0x21, 3, 0x20, 2, 0x21, 1, 0x20, 3, 0x21, 2,
			0x20, 4, k, 1, add, 0x21, 4, 0x0c, 0, 0x0b, 0x0b,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 5, 0x20, 4, add, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, typ, typ, typ}, []wasm.ValType{typ}, body})
		for _, workers := range []int{1, 4} {
			for _, n := range []uint64{0, 1, 2, 4} {
				for _, seed := range []uint64{7, ^uint64(0)} {
					a, b, r, total, steps := uint64(13), uint64(8), fold(seed), uint64(0), uint64(0)
					for i := uint64(0); i < n; i++ {
						total = fold(total + r)
						for b != 0 {
							r = a % b
							a, b = b, r
							steps++
						}
					}
					var stats ModuleStats
					opts := CompileOptions{Workers: workers}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, n, 13, 8, seed)
					want := fold(total + steps)
					if err != nil || fold(got) != want {
						t.Fatalf("wide=%t n=%d seed=%x got=%x err=%v want=%x", wide, n, seed, got, err, want)
					}
					if diagnosticsEnabled && !profileEnabled && stats.Funcs[0].Peephole["scalar-loop-unroll-remainder-pair"] == 0 {
						t.Fatalf("outer-loop case did not exercise remainder rotation: %v", stats.Funcs[0].Peephole)
					}
					if diagnosticsEnabled && stats.Funcs[0].Peephole["scalar-loop-remainder-dead-local"] != 0 {
						t.Fatal("outer backedge incorrectly proved dead")
					}
				}
			}
		}
	}
}
