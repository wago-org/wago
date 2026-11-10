//go:build amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"
)

func floatRankingCode(values ...float64) []byte {
	var code []byte
	for _, value := range values {
		code = append(code, 0x44)
		code = binary.LittleEndian.AppendUint64(code, math.Float64bits(value))
		code = append(code, 0x1a)
	}
	return append(code, 0x0b)
}

func TestFloatConstRankingChoice(t *testing.T) {
	for _, tc := range []struct {
		values  []float64
		want    [2]float64
		changed bool
	}{
		{[]float64{101, 102, .5, .01, .5, .01, .01}, [2]float64{.01, .5}, true},
		{[]float64{1.25, 2.5, 5}, [2]float64{1.25, 2.5}, false},
		{[]float64{0, 1, 0, 0, 0, 0, 0, 0, 1, 1, 1, .5, .5, .5, .5, .5}, [2]float64{0, 1}, false},
	} {
		var f fn
		choice, n, changed := f.rankFloatConsts(floatRankingCode(tc.values...))
		if n != 2 || changed != tc.changed {
			t.Fatal(n, changed, tc)
		}
		for i, value := range tc.want {
			if choice[i].typ != mtF64 || uint64(choice[i].cval) != math.Float64bits(value) {
				t.Fatal(i, choice, tc)
			}
		}
	}
}

func TestFloatConstRankingOverflow(t *testing.T) {
	values := make([]float64, 33)
	for i := range values {
		values[i] = float64(i + 1)
	}
	for i := 0; i < 40; i++ {
		values = append(values, 2)
	}
	var f fn
	choice, n, changed := f.rankFloatConsts(floatRankingCode(values...))
	if n != 2 || changed || uint64(choice[0].cval) != math.Float64bits(1) || uint64(choice[1].cval) != math.Float64bits(2) {
		t.Fatal(choice, n, changed)
	}
}

func TestFloatConstRankingExactTypeAndBits(t *testing.T) {
	var code []byte
	appendF32 := func(bits uint32) {
		code = append(code, 0x43)
		code = binary.LittleEndian.AppendUint32(code, bits)
		code = append(code, 0x1a)
	}
	appendF64 := func(bits uint64) {
		code = append(code, 0x44)
		code = binary.LittleEndian.AppendUint64(code, bits)
		code = append(code, 0x1a)
	}
	appendF32(0)
	appendF64(0)
	for i := 0; i < 3; i++ {
		appendF32(0x80000000)
		appendF64(0x7ff0000000004321)
	}
	code = append(code, 0x0b)
	var f fn
	choice, n, changed := f.rankFloatConsts(code)
	if n != 2 || !changed || choice[0].typ != mtF32 || uint64(choice[0].cval) != 0x80000000 || choice[1].typ != mtF64 || uint64(choice[1].cval) != 0x7ff0000000004321 {
		t.Fatal(choice, n, changed)
	}
	// The fixed candidate packet should not escape or allocate.
	if got := testing.AllocsPerRun(100, func() { f.rankFloatConsts(code) }); got != 0 {
		t.Fatal("ranking allocations", got)
	}
}
