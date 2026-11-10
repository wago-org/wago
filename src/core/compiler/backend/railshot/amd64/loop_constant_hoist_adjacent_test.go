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

func constantAdjacentFixture(t *testing.T, bits uint64, op byte, left bool) *wasm.Module {
	body := []byte{1, 2, 0x7c, 0x03, 0x40}
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], bits)
	literal := append([]byte{0x44}, raw[:]...)
	for lane := byte(0); lane < 2; lane++ {
		body = append(body, 0x20, 0)
		load := []byte{0x20, 1, 0x2b, 0, lane * 8}
		if left {
			body = append(body, literal...)
			body = append(body, load...)
		} else {
			body = append(body, load...)
			body = append(body, literal...)
		}
		body = append(body, op, 0x22, 5+lane, 0x39, 0, lane*8)
	}
	for _, i := range []byte{0, 1} {
		body = append(body, 0x20, i, 0x41, 16, 0x6a, 0x21, i)
	}
	body = append(body, 0x20, 3, 0x41, 1, 0x6a, 0x22, 3, 0x20, 4, 0x47, 0x0d, 0, 0x0b, 0x20, 5, 0xbd, 0x20, 6, 0xbd, 0x42, 17, 0x89, 0x85, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestLoopConstantHoistAdjacentOwnershipAndTraps(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, adjacent, force, hoist, prefix, forms := regionLoopEnabled, regionAdjacentEnabled, regionLoopTestFast, regionConstantHoistEnabled, regionInvariantPrefixEnabled, regionLoopMemForms
	defer func() {
		regionLoopEnabled, regionAdjacentEnabled, regionLoopTestFast, regionConstantHoistEnabled, regionInvariantPrefixEnabled, regionLoopMemForms = old, adjacent, force, hoist, prefix, forms
	}()
	regionLoopEnabled, regionInvariantPrefixEnabled, regionLoopMemForms = false, false, true
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for op := byte(0xa0); op <= 0xa3; op++ {
			for _, left := range []bool{false, true} {
				for _, bits := range []uint64{0x8000000000000000, math.Float64bits(1.5), 0x7ff8000000000123, 0x7ff0000000000432} {
					m := constantAdjacentFixture(t, bits, op, left)
					for _, tc := range []struct {
						dst, src, n uint64
						trap        bool
					}{{1024, 128, 1, false}, {1024, 132, 3, false}, {128, 128, 3, false}, {132, 128, 3, false}, {1024, 65520, 1, false}, {1024, 65528, 1, true}} {
						var want uint64
						var expected []byte
						var trapped bool
						for mode := 0; mode < 3; mode++ {
							regionAdjacentEnabled, regionConstantHoistEnabled = mode != 0, mode == 2
							regionLoopTestFast = mode != 0 && !tc.trap && tc.dst != 132
							init := func(mem []byte) {
								for i := uint64(0); i < 2*tc.n; i++ {
									if tc.src+8*i+8 <= uint64(len(mem)) {
										binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(float64(i+2)))
									}
								}
							}
							var stats ModuleStats
							got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: &stats}, init, tc.dst, tc.src, 0, 0, tc.n)
							if mode == 0 {
								want, expected, trapped = got, append([]byte(nil), mem...), err != nil
							} else if (err != nil) != trapped || (!trapped && got != want) || !bytes.Equal(mem, expected) {
								t.Fatal("adjacent constant changed output/ownership/trap effects", features, op, left, bits, tc, mode, got, want, err)
							}
							if (err != nil) != tc.trap {
								t.Fatal("wrong trap", tc, mode, err)
							}
							if mode == 2 && stats.Funcs[0].Peephole["region-loop-constant-hoist"] != 1 {
								t.Fatal("constant policy not emitted", stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
