//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestVectorMapLateRejectionPreservesScalarContinuation(t *testing.T) {
	wide, pair, zero, tail, force := regionWideIndependentEnabled, regionLoopEnabled, regionZeroCounterEnabled, regionWideCheckedTailEnabled, regionLoopTestFast
	defer func() {
		regionWideIndependentEnabled, regionLoopEnabled, regionZeroCounterEnabled, regionWideCheckedTailEnabled, regionLoopTestFast = wide, pair, zero, tail, force
	}()
	// Admit the wide candidate, then reject it after compiling the original
	// body: neither checked wide tails nor the paired alternative is enabled.
	regionLoopEnabled, regionZeroCounterEnabled = false, false
	regionWideCheckedTailEnabled, regionLoopTestFast = false, false
	m := wideIndependentFixture(t, 0xa2, false)
	for _, compact := range []bool{false, true} {
		for _, tc := range []struct{ dst, src, count uint64 }{
			{128, 256, 1}, {128, 256, 3}, {128, 256, 8},
			{128, 128, 8}, {132, 128, 8},
			{65520, 256, 4}, // Two stores survive before the third traps.
			{128, 65520, 4}, // A later load traps after earlier stores.
		} {
			var want uint64
			var wantMemory []byte
			var wantError string
			for _, candidate := range []bool{false, true} {
				regionWideIndependentEnabled = candidate
				var stats ModuleStats
				init := func(mem []byte) {
					for i := uint64(0); i < tc.count && tc.src+8*i+8 <= uint64(len(mem)); i++ {
						binary.LittleEndian.PutUint64(mem[tc.src+8*i:], math.Float64bits(float64(i+1)))
					}
				}
				got, memory, err := runMemAmd64WithOptions(t, m, CompileOptions{
					AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline,
					CompactNative: compact, Stats: optionalTestStats(&stats),
				}, init, tc.dst, tc.src, 0, tc.count, math.Float64bits(2), math.Float64bits(1.5))
				if !candidate {
					want, wantMemory, wantError = got, memory, fmt.Sprint(err)
				} else {
					if got != want || !bytes.Equal(memory, wantMemory) || fmt.Sprint(err) != wantError {
						t.Fatalf("scalar continuation differs: compact=%v case=%+v got=%x want=%x error=%v wantError=%s", compact, tc, got, want, err, wantError)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["region-loop-exit-reject"] != 1 || stats.Funcs[0].Peephole["region-loop-fast"] != 0) {
						t.Fatal("fixture did not reach late rejection", stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
