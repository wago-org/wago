//go:build amd64 && linux && wago_experimental_cooperative_linear_sum

package amd64

import (
	"encoding/binary"
	"testing"
)

func TestCooperativeLinearSumValuesAndFallback(t *testing.T) {
	m := linearSumLoopModuleAMD64(t)
	setup := func(mem []byte) {
		for i := 0; i < len(mem)/8; i++ {
			binary.LittleEndian.PutUint64(mem[i*8:], ^uint64(i)*0x9e3779b97f4a7c15)
		}
	}
	for _, n := range []uint64{0, 1, 2, 3, 4, 5, 7, 8, 9, 31, 63, 64, 65, 66, 67, 68, 127, 128, 129, 130, 131, 132, 511, 8192, 8193, 1<<32 - 1} {
		var value uint64
		var failed bool
		for _, enabled := range []bool{false, true} {
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Interruptible: true, Optimizations: map[string]bool{"linear-sum-loop": enabled}}, setup, n)
			if !enabled {
				value, failed = got, err != nil
			} else if (err != nil) != failed || err == nil && got != value {
				t.Fatalf("n=%d result %x/%x errors %v/%v", n, got, value, err, failed)
			}
		}
	}
}

func TestCooperativeLinearSumSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(linearSumLoopModuleAMD64(t), CompileOptions{Interruptible: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if stats.Funcs[0].Peephole["cooperative-linear-sum-unroll4"] != 1 {
		t.Fatal(stats.Funcs[0].Peephole)
	}
	if stats.Funcs[0].Peephole["counted-loop-bounds-hoist"] != 0 {
		t.Fatal("cooperative range proof must not hoist a trap")
	}
	if stats.Funcs[0].Peephole["cooperative-linear-sum-poll"] != 3 {
		t.Fatal("need first/group/tail poll sites", stats.Funcs[0].Peephole)
	}
}

func TestCooperativeLinearSumShapeIsNotHoistedProof(t *testing.T) {
	f := fn{interruptible: true, linearSumLoop: 1 | 3<<16, linearSumLoopDepth: 1}
	if f.hoistedLoopBoundsCover(1, 0) {
		t.Fatal("shape candidate suppressed first/fallback bounds check")
	}
}
