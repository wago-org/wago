//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"math"
	"testing"
)

func TestFloatConstRankReplacesOnlyDecisivelyWeakSlot(t *testing.T) {
	old := floatConstSlotRankingEnabled
	defer func() { floatConstSlotRankingEnabled = old }()
	var code []byte
	add := func(value float64, count int) {
		for i := 0; i < count; i++ {
			code = appendF64ConstForCacheTest(code, value)
			code = append(code, 0x1a)
		}
	}
	add(0, 1)
	add(4.0/9.0, 1)
	add(0, 18)
	add(3, 9)
	add(1, 9)
	code = append(code, 0x0b)
	var common Reg
	for _, enabled := range []bool{false, true} {
		floatConstSlotRankingEnabled = enabled
		f := fn{a: &a64.Asm{}, s: newStack()}
		f.preloadFloatConsts(code)
		if len(f.fconsts) != 2 || uint64(f.fconsts[0].bits) != math.Float64bits(0) {
			t.Fatalf("enabled%v cache%v", enabled, f.fconsts)
		}
		want := 4.0 / 9.0
		if enabled {
			want = 3
		}
		if uint64(f.fconsts[1].bits) != math.Float64bits(want) {
			t.Fatalf("enabled%v second%v want%v", enabled, math.Float64frombits(uint64(f.fconsts[1].bits)), want)
		}
		if enabled && f.fconsts[0].reg != common {
			t.Fatal("unchanged entry moved registers")
		}
		common = f.fconsts[0].reg
	}
}

func TestFloatConstRankKeepsSlotsAndThreshold(t *testing.T) {
	for _, tc := range []struct {
		counts            [4]int
		first, best, want [2]int
	}{
		{[4]int{19, 1, 9, 9}, [2]int{0, 1}, [2]int{0, 2}, [2]int{0, 2}},
		{[4]int{1, 19, 9, 9}, [2]int{0, 1}, [2]int{1, 2}, [2]int{2, 1}},
		{[4]int{5, 4, 7, 0}, [2]int{0, 1}, [2]int{2, 0}, [2]int{0, 1}},
		{[4]int{5, 4, 8, 0}, [2]int{0, 1}, [2]int{2, 0}, [2]int{0, 2}},
		{[4]int{5, 4, 8, 9}, [2]int{0, 1}, [2]int{3, 2}, [2]int{0, 1}},
		{[4]int{5, 4, 0, 0}, [2]int{0, 1}, [2]int{0, 1}, [2]int{0, 1}},
	} {
		var candidates [4]floatConstCandidate
		for i, n := range tc.counts {
			candidates[i].n = n
		}
		if got := rankSingleFloatConstReplacement(candidates[:], tc.first, tc.best); got != tc.want {
			t.Fatalf("counts%v first%v best%v got%v want%v", tc.counts, tc.first, tc.best, got, tc.want)
		}
	}
}

func TestFloatConstRankLoopExecution(t *testing.T) {
	old := floatConstSlotRankingEnabled
	defer func() { floatConstSlotRankingEnabled = old }()
	body := []byte{1, 1, 0x7c, 0x20, 0}
	body = appendF64ConstForCacheTest(body, 0)
	body = append(body, 0xa2)
	body = appendF64ConstForCacheTest(body, 4.0/9.0)
	body = append(body, 0xa0, 0x21, 2, 0x02, 0x40, 0x20, 1, 0x45, 0x0d, 0, 0x03, 0x40)
	for i := 0; i < 9; i++ {
		body = append(body, 0x20, 2)
		body = appendF64ConstForCacheTest(body, 3)
		body = append(body, 0xa2)
		body = appendF64ConstForCacheTest(body, 0)
		body = append(body, 0xa0, 0x21, 2)
	}
	body = append(body, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x0b, 0x20, 2, 0xbd, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.I64}, body)
	for _, enabled := range []bool{false, true} {
		floatConstSlotRankingEnabled = enabled
		for _, count := range []uint64{0, 1, 2, 7, 15, 1 << 32} {
			want := float64(4.0 / 9.0)
			for n := uint32(0); n < uint32(count); n++ {
				for j := 0; j < 9; j++ {
					want *= 3
				}
			}
			for _, input := range []float64{0, math.Copysign(0, -1), math.SmallestNonzeroFloat64, 1.1, -1e300, math.Inf(1), math.Float64frombits(0x7ff8000000001234)} {
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, math.Float64bits(input), count)
				nan := math.IsInf(input, 0) || math.IsNaN(input)
				if err != nil || nan && !math.IsNaN(math.Float64frombits(got)) || !nan && got != math.Float64bits(want) {
					t.Fatalf("enabled%v count%d input%v got%x want%x err%v", enabled, count, input, got, math.Float64bits(want), err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["fp-cache-slot-ranking"] != 0) != enabled {
					t.Fatalf("enabled%v stats%v", enabled, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}
