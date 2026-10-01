//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"math"
	"testing"
)

func TestWideIndependentGroupsRemaindersAndExitState(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, whole, tail, force, zero := regionWideIndependentEnabled, regionLoopEnabled, regionWideCheckedTailEnabled, regionLoopTestFast, regionZeroCounterEnabled
	defer func() {
		regionWideIndependentEnabled, regionLoopEnabled, regionWideCheckedTailEnabled, regionLoopTestFast, regionZeroCounterEnabled = saved, whole, tail, force, zero
	}()
	regionLoopEnabled = false
	regionWideCheckedTailEnabled = true
	regionZeroCounterEnabled = true
	bits := []uint64{0, 0x8000000000000000, 1, math.Float64bits(-1), 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000123, 0x7ff0000000000042, math.Float64bits(3)}
	for _, op := range []byte{0xa0, 0xa1, 0xa2, 0xa3} {
		for _, zeroCount := range []bool{false, true} {
			m := wideIndependentFixture(t, op, zeroCount)
			for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
				for n := uint64(1); n <= 9; n++ {
					for _, src := range []uint64{128, 256, 260, 65536 - 8*n} {
						var want uint64
						var expected []byte
						for _, on := range []bool{false, true} {
							regionWideIndependentEnabled = on
							regionLoopTestFast = on && features&shared.AMD64AVX != 0
							start, limit := uint64(0), n
							if zeroCount {
								start, limit = uint64(uint32(0-uint32(n))), 0
							}
							setup := func(mem []byte) {
								for i := uint64(0); i < n; i++ {
									binary.LittleEndian.PutUint64(mem[src+8*i:], bits[i])
								}
							}
							var stats ModuleStats
							got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: &stats, AMD64FeaturesSet: true, AMD64Features: features}, setup, 128, src, start, limit, math.Float64bits(2), 0x8000000000000000)
							if err != nil {
								t.Fatalf("op=%x zero=%v features=%v n=%v src=%v on=%v: %v stats=%v", op, zeroCount, features, n, src, on, err, stats.Funcs[0].Peephole)
							}
							if wantWide := on && features&shared.AMD64AVX != 0; (stats.Funcs[0].Peephole["region-loop-wide-independent"] != 0) != wantWide {
								t.Fatal("wide execution fixture was not admitted", features, on, stats.Funcs[0].Peephole)
							}
							if !on {
								want, expected = got, append([]byte(nil), mem...)
							} else if got != want || !bytes.Equal(mem, expected) {
								t.Fatalf("group/tail changed bits or exit state: op=%x zero=%v features=%v n=%v src=%v got=%x want=%x", op, zeroCount, features, n, src, got, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestWideIndependentAliasAndTrapFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved, whole, tail, force := regionWideIndependentEnabled, regionLoopEnabled, regionWideCheckedTailEnabled, regionLoopTestFast
	defer func() {
		regionWideIndependentEnabled, regionLoopEnabled, regionWideCheckedTailEnabled, regionLoopTestFast = saved, whole, tail, force
	}()
	regionLoopEnabled = false
	regionWideCheckedTailEnabled = true
	regionLoopTestFast = false
	m := wideIndependentFixture(t, 0xa2, false)
	for _, tc := range []struct {
		src, n uint64
		trap   bool
	}{{120, 8, false}, {132, 8, false}, {136, 8, false}, {65504, 5, true}, {65512, 4, true}} {
		var want uint64
		var expected []byte
		var trapped bool
		var wantErr string
		for _, on := range []bool{false, true} {
			regionWideIndependentEnabled = on
			setup := func(mem []byte) {
				for i := uint64(0); i < tc.n && tc.src+8*i+8 <= uint64(len(mem)); i++ {
					binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(float64(i+1)))
				}
			}
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline}, setup, 128, tc.src, 0, tc.n, math.Float64bits(2), math.Float64bits(1.5))
			if (err != nil) != tc.trap {
				t.Fatal("wrong trap", tc, on, err)
			}
			if !on {
				want, expected, trapped = got, append([]byte(nil), mem...), err != nil
				if err != nil {
					wantErr = err.Error()
				}
			} else if (err != nil) != trapped || (trapped && err.Error() != wantErr) || (!trapped && got != want) || !bytes.Equal(mem, expected) {
				t.Fatal("fallback changed memory, trap, or result", tc)
			}
		}
	}
}
