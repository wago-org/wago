//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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
							regionAdjacentEnabled, regionLoopTestFast, regionLoopMemForms = on, on, on
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
						regionLoopTestFast = mode != 0 && tc.inv != tc.dst && !tc.trap
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

func wideInputAdjacentFixture(t *testing.T, op byte, left bool) *wasm.Module {
	body := []byte{1, 2, 0x7c, 0x03, 0x40}
	for lane := byte(0); lane < 2; lane++ {
		body = append(body, 0x20, 0)
		load := []byte{0x20, 1, 0x2b, 0, lane * 8}
		if left {
			body = append(body, 0x20, 5)
			body = append(body, load...)
		} else {
			body = append(body, load...)
			body = append(body, 0x20, 5)
		}
		body = append(body, op, 0x22, 6+lane, 0x39, 0, lane*8)
	}
	for _, i := range []byte{0, 1} {
		body = append(body, 0x20, i, 0x41, 16, 0x6a, 0x21, i)
	}
	body = append(body, 0x20, 3, 0x41, 1, 0x6a, 0x22, 3, 0x20, 4, 0x47, 0x0d, 0, 0x0b, 0x20, 6, 0xbd, 0x20, 7, 0xbd, 0x42, 17, 0x89, 0x85, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.F64}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestWideAdjacentLoopInputAndLiteralBroadcast(t *testing.T) {
	requireCompilerDiagnostics(t)
	oldWide, oldAdjacent, oldForce, oldLoop := regionWideAdjacentEnabled, regionAdjacentEnabled, regionLoopTestFast, regionLoopEnabled
	defer func() {
		regionWideAdjacentEnabled, regionAdjacentEnabled, regionLoopTestFast, regionLoopEnabled = oldWide, oldAdjacent, oldForce, oldLoop
	}()
	regionLoopEnabled = false
	for op := byte(0xa0); op <= 0xa3; op++ {
		for _, left := range []bool{false, true} {
			for _, bits := range []uint64{0x8000000000000000, math.Float64bits(1.5), 0x7ff8000000001234, 0x7ff0000000004321} {
				for _, input := range []bool{false, true} {
					m := constantAdjacentFixture(t, bits, op, left)
					if input {
						m = wideInputAdjacentFixture(t, op, left)
					}
					for _, n := range []uint64{1, 2, 3, 4} {
						var want uint64
						var expected []byte
						for mode := 0; mode < 3; mode++ {
							regionAdjacentEnabled, regionWideAdjacentEnabled, regionLoopTestFast = mode != 0, mode == 2, mode != 0
							init := func(mem []byte) {
								for i := uint64(0); i < 2*n; i++ {
									binary.LittleEndian.PutUint64(mem[132+8*i:], math.Float64bits(float64(i+2)))
								}
							}
							args := []uint64{1024, 132, 0, 0, n}
							if input {
								args = append(args, bits)
							}
							var stats ModuleStats
							got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64AVX, Stats: &stats}, init, args...)
							if err != nil {
								t.Fatal(op, left, bits, input, n, mode, err)
							}
							if mode == 0 {
								want, expected = got, append([]byte(nil), mem...)
							} else if got != want || !bytes.Equal(mem, expected) {
								t.Fatal("broadcast changed result or memory", op, left, bits, input, n, mode)
							}
							if mode == 2 && stats.Funcs[0].Peephole["region-loop-wide-odd-pair"] != 1 {
								t.Fatal("wide odd dispatch missing", stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
