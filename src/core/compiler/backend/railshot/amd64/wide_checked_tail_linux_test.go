//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestWideCheckedTailExecutionAndCodeSize(t *testing.T) {
	requireCompilerDiagnostics(t)
	oldWide, oldChecked, oldAdjacent, oldForce, oldLoop := regionWideAdjacentEnabled, regionWideCheckedTailEnabled, regionAdjacentEnabled, regionLoopTestFast, regionLoopEnabled
	defer func() {
		regionWideAdjacentEnabled, regionWideCheckedTailEnabled, regionAdjacentEnabled, regionLoopTestFast, regionLoopEnabled = oldWide, oldChecked, oldAdjacent, oldForce, oldLoop
	}()
	regionWideAdjacentEnabled, regionAdjacentEnabled, regionLoopTestFast, regionLoopEnabled = true, true, true, false
	admitted := 0
	for op := byte(0xa0); op <= 0xa3; op++ {
		m := adjacentLoopFixture(t, op)
		for _, n := range []uint64{1, 2, 3, 4, 5, 6, 7} {
			var expected []byte
			var want uint64
			var pairedBytes int
			for _, checked := range []bool{false, true} {
				regionWideCheckedTailEnabled = checked
				init := func(mem []byte) {
					for i := uint64(0); i < 2*n; i++ {
						binary.LittleEndian.PutUint64(mem[128+8*i:], math.Float64bits(float64(i+3)))
						binary.LittleEndian.PutUint64(mem[512+8*i:], math.Float64bits(2.25))
					}
				}
				var stats ModuleStats
				got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64AVX, Stats: &stats}, init, 1024, 128, 512, 0, n)
				if err != nil {
					t.Fatal(op, n, checked, err)
				}
				if !checked {
					want, expected = got, append([]byte(nil), mem...)
					pairedBytes = stats.Funcs[0].CodeBytes
				} else {
					if got != want || !bytes.Equal(mem, expected) {
						t.Fatal("checked tail changed exit or memory", op, n, got, want)
					}
					if stats.Funcs[0].Peephole["region-loop-wide-checked-tail"] == 1 {
						admitted++
						if stats.Funcs[0].CodeBytes >= pairedBytes {
							t.Fatal("checked tail did not shrink function", stats.Funcs[0].CodeBytes, pairedBytes)
						}
						if stats.Funcs[0].Peephole["region-loop-wide-paired-tail"] != 0 {
							t.Fatal("both tails emitted")
						}
					}
				}
			}
		}
	}
	if admitted == 0 {
		t.Fatal("no checked-tail fixture admitted")
	}
}
