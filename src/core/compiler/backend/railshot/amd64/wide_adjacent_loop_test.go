//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestWideAdjacentLoopArithmeticAndExitLanes(t *testing.T) {
	requireCompilerDiagnostics(t)
	wide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = true
	defer func() { regionWideAdjacentEnabled = wide }()
	saved, old, force, forms := regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms
	defer func() {
		regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms = saved, old, force, forms
	}()
	regionLoopEnabled = false
	for op := byte(0xa0); op <= 0xa3; op++ {
		m := adjacentLoopFixture(t, op)
		for _, features := range []shared.AMD64Features{0, shared.AMD64AVX, shared.AMD64ModernBaseline} {
			for _, src := range []uint64{512, 516, 65504} {
				for _, n := range []uint64{1, 2, 3, 4} {
					if src == 65504 && n > 2 {
						continue
					}
					for _, bits := range []uint64{0x8000000000000000, 0x3ff4000000000000, 0x7ff8000000001234, 0x7ff0000000004321} {
						var want uint64
						var expected []byte
						for _, on := range []bool{false, true} {
							regionAdjacentEnabled, regionLoopTestFast, regionLoopMemForms = on, on && (features&shared.AMD64AVX == 0 || n%2 == 0), on
							init := func(mem []byte) {
								for i := uint64(0); i < 2*n; i++ {
									binary.LittleEndian.PutUint64(mem[128+8*i:], math.Float64bits(float64(i+3)))
									v := bits
									if i%2 != 0 {
										v = math.Float64bits(2.25)
									}
									binary.LittleEndian.PutUint64(mem[src+8*i:], v)
								}
							}
							var stats ModuleStats
							got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, 1024, 128, src, 0, n)
							if err != nil {
								t.Fatal(op, features, src, n, bits, on, err)
							}
							if !on {
								want, expected = got, append([]byte(nil), mem...)
							} else {
								if got != want || !bytes.Equal(mem, expected) {
									t.Fatal("paired output/exit mismatch", op, features, src, n, bits, got, want)
								}
								if (stats.Funcs[0].Peephole["region-loop-wide-adjacent"] == 1) != (features&shared.AMD64AVX != 0) {
									t.Fatal("wrong AVX admission", features, stats.Funcs[0].Peephole)
								}
								if stats.Funcs[0].Peephole["region-loop-fast"] != 1 {
									t.Fatal("no adjacent fast path", stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestWideAdjacentLoopAliasAndBoundsFallback(t *testing.T) {
	wide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = true
	defer func() { regionWideAdjacentEnabled = wide }()
	saved, old, force, forms := regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms
	defer func() {
		regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms = saved, old, force, forms
	}()
	regionLoopEnabled = false
	m := adjacentLoopFixture(t, 0xa0)
	for _, tc := range []struct {
		dst, src, n uint64
		trap        bool
	}{{1024, 512, 4, false}, {128, 512, 4, false}, {136, 512, 4, false}, {132, 512, 4, false}, {1024, 65512, 2, true}, {1024, 512, 3, false}, {128, 512, 3, false}, {136, 512, 3, false}, {132, 512, 3, false}, {1024, 65528, 1, true}} {
		var want uint64
		var expected []byte
		var wantTrap bool
		for _, on := range []bool{false, true} {
			regionAdjacentEnabled, regionLoopTestFast, regionLoopMemForms = on, false, on
			init := func(mem []byte) {
				for i := uint64(0); i < 2*tc.n; i++ {
					binary.LittleEndian.PutUint64(mem[128+8*i:], math.Float64bits(float64(i+3)))
					if tc.src+8*i+8 <= uint64(len(mem)) {
						binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(7))
					}
				}
			}
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, init, tc.dst, 128, tc.src, 0, tc.n)
			if !on {
				want, expected, wantTrap = got, append([]byte(nil), mem...), err != nil
			} else if (err != nil) != wantTrap || (!wantTrap && got != want) || !bytes.Equal(mem, expected) {
				t.Fatal("fallback mismatch", tc, on, got, want, err)
			}
			if (err != nil) != tc.trap {
				t.Fatal("wrong trap", tc, err)
			}
			if tc.trap && binary.LittleEndian.Uint64(mem[tc.dst:]) != math.Float64bits(10) {
				t.Fatal("trap erased first scalar store")
			}
		}
	}
}

func TestWideAdjacentLoopInvariantBroadcast(t *testing.T) {
	requireCompilerDiagnostics(t)
	wide := regionWideAdjacentEnabled
	regionWideAdjacentEnabled = true
	defer func() { regionWideAdjacentEnabled = wide }()
	saved, old, force, forms, hoist := regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms, regionInvariantPrefixEnabled
	defer func() {
		regionAdjacentEnabled, regionLoopEnabled, regionLoopTestFast, regionLoopMemForms, regionInvariantPrefixEnabled = saved, old, force, forms, hoist
	}()
	regionLoopEnabled = false
	for op := byte(0xa0); op <= 0xa3; op++ {
		m := invariantPrefixFixture(t, op)
		for _, features := range []shared.AMD64Features{shared.AMD64AVX, shared.AMD64ModernBaseline} {
			for _, tc := range []struct {
				dst, src, inv, n uint64
				trap             bool
			}{{1024, 128, 512, 2, false}, {1024, 132, 516, 4, false}, {1024, 128, 65528, 4, false}, {1024, 132, 516, 3, false}, {1024, 128, 65528, 3, false}, {1024, 128, 1024, 3, false}, {1024, 65528, 512, 1, true}} {
				for _, value := range []uint64{0x8000000000000000, math.Float64bits(2.25), 0x7ff8000000001234, 0x7ff0000000004321} {
					var want uint64
					var expected []byte
					var wantTrap bool
					for mode := 0; mode < 3; mode++ {
						regionAdjacentEnabled = mode != 0
						regionInvariantPrefixEnabled = mode == 2
						regionLoopMemForms = true
						regionLoopTestFast = mode != 0 && tc.inv != tc.dst && !tc.trap && tc.n%2 == 0
						init := func(mem []byte) {
							for i := uint64(0); i < 2*tc.n; i++ {
								if tc.src+8*i+8 <= uint64(len(mem)) {
									binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(float64(i+2)))
								}
							}
							binary.LittleEndian.PutUint64(mem[tc.inv:], value)
						}
						var stats ModuleStats
						got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, tc.dst, tc.src, tc.inv, 0, tc.n)
						if mode == 0 {
							want, expected, wantTrap = got, append([]byte(nil), mem...), err != nil
						} else if (err != nil) != wantTrap || (!wantTrap && got != want) || !bytes.Equal(mem, expected) {
							t.Fatal("invariant prefix mismatch", op, features, tc, value, mode, got, want, err)
						}
						if (err != nil) != tc.trap {
							t.Fatal("wrong trap", tc, mode, err)
						}
						if mode == 2 && stats.Funcs[0].Peephole["region-loop-invariant-prefix"] != 1 {
							t.Fatal("prefix not emitted", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
