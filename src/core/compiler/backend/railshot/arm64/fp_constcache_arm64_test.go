//go:build arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func appendF64ConstForCacheTest(code []byte, value float64) []byte {
	code = append(code, 0x44)
	return binary.LittleEndian.AppendUint64(code, math.Float64bits(value))
}

func expandFPImmediateForTest(imm uint8, f64 bool) uint64 {
	sign := uint64(imm >> 7)
	b6 := uint64(imm>>6) & 1
	lowExp := uint64(imm>>4) & 3
	frac := uint64(imm & 15)
	if f64 {
		exp := (b6^1)<<10 | b6*0xff<<2 | lowExp
		return sign<<63 | exp<<52 | frac<<48
	}
	exp := (b6^1)<<7 | b6*0x1f<<2 | lowExp
	return sign<<31 | exp<<23 | frac<<19
}

func TestEncodeFPImmediateArm64Exhaustive(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		for n := 0; n < 256; n++ {
			imm := uint8(n)
			bits := expandFPImmediateForTest(imm, f64)
			got, ok := encodeFPImmediate(bits, f64)
			if !ok || got != imm {
				t.Fatalf("f64=%t imm=%#x bits=%#x encoded=(%#x,%t)", f64, imm, bits, got, ok)
			}
			if _, ok := encodeFPImmediate(bits|1, f64); ok {
				t.Fatalf("f64=%t admitted nonzero low fraction for %#x", f64, bits|1)
			}
		}
	}
	for _, tc := range []struct {
		bits uint64
		f64  bool
	}{
		{0, false}, {0x80000000, false}, {math.Float64bits(0), true}, {math.Float64bits(math.Copysign(0, -1)), true},
		{uint64(math.Float32bits(0.1)), false}, {math.Float64bits(0.1), true},
	} {
		if _, ok := encodeFPImmediate(tc.bits, tc.f64); ok {
			t.Fatalf("f64=%t unexpectedly admitted bits %#x", tc.f64, tc.bits)
		}
	}
}

func TestFPImmediateConstExecArm64(t *testing.T) {
	for _, value := range []float64{0, 0.125, 0.5, 1, 2, -1, -31} {
		t.Run(fmt.Sprintf("%g", value), func(t *testing.T) {
			bits := math.Float64bits(value)
			body := []byte{0x00, 0x44}
			body = binary.LittleEndian.AppendUint64(body, bits)
			body = append(body, 0xbd, 0x0b) // i64.reinterpret_f64; end
			m := mod1(t, nil, []wasm.ValType{wasm.I64}, body)
			stats := &ModuleStats{}
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Stats: stats, Optimizations: map[string]bool{"fp-immediate-const": true}})
			if err != nil {
				t.Fatal(err)
			}
			if got != bits {
				t.Fatalf("result bits = %#x, want %#x", got, bits)
			}
			key := "fp-immediate-const"
			if value == 0 {
				key = "fp-immediate-zero"
			}
			if hits := stats.Funcs[0].Peephole[key]; hits == 0 {
				t.Fatalf("%s did not fire: %v", key, stats.Funcs[0].Peephole)
			}
		})
	}
}

func TestPreloadFloatConstsChoosesMostFrequent(t *testing.T) {
	var code []byte
	for _, value := range []float64{
		101, 102, // one-shot setup constants used to occupy the cache
		0.5, 0.01, 0.5, 0.01, 0.01,
	} {
		code = appendF64ConstForCacheTest(code, value)
		code = append(code, 0x1a) // drop
	}
	code = append(code, 0x0b) // end

	f := &fn{a: &a64.Asm{}, s: newStack()}
	f.preloadFloatConsts(code)
	if len(f.fconsts) != 2 {
		t.Fatalf("cached constants = %d, want 2", len(f.fconsts))
	}
	if got, want := uint64(f.fconsts[0].bits), math.Float64bits(0.01); got != want {
		t.Fatalf("first cached constant bits = %#x, want %#x", got, want)
	}
	if got, want := uint64(f.fconsts[1].bits), math.Float64bits(0.5); got != want {
		t.Fatalf("second cached constant bits = %#x, want %#x", got, want)
	}
}

func TestPreloadFloatConstsPreservesFirstSeenTie(t *testing.T) {
	var code []byte
	for _, value := range []float64{1.25, 2.5, 5} {
		code = appendF64ConstForCacheTest(code, value)
	}
	code = append(code, 0x0b)

	f := &fn{a: &a64.Asm{}, s: newStack()}
	f.preloadFloatConsts(code)
	for i, want := range []float64{1.25, 2.5} {
		if got := uint64(f.fconsts[i].bits); got != math.Float64bits(want) {
			t.Fatalf("cached constant %d bits = %#x, want %#x", i, got, math.Float64bits(want))
		}
	}
}

func TestPreloadFloatConstsRejectsMarginalReordering(t *testing.T) {
	// This mirrors spectralnorm's static shape: zero and one are the first two
	// constants, while half occurs once more often than one but outside the hot
	// inner loops. A marginal 12:11 combined-frequency advantage is not enough to
	// perturb the established cache/register assignment.
	var code []byte
	appendUses := func(value float64, n int) {
		for i := 0; i < n; i++ {
			code = appendF64ConstForCacheTest(code, value)
			code = append(code, 0x1a) // drop
		}
	}
	code = appendF64ConstForCacheTest(code, 0)
	code = append(code, 0x1a)
	code = appendF64ConstForCacheTest(code, 1)
	code = append(code, 0x1a)
	appendUses(0, 6)
	appendUses(1, 3)
	appendUses(0.5, 5)
	code = append(code, 0x0b)

	f := &fn{a: &a64.Asm{}, s: newStack()}
	f.preloadFloatConsts(code)
	for i, want := range []float64{0, 1} {
		if got := uint64(f.fconsts[i].bits); got != math.Float64bits(want) {
			t.Fatalf("cached constant %d bits = %#x, want conservative first-seen %#x", i, got, math.Float64bits(want))
		}
	}
}

func TestPreloadFloatConstsFallsBackOnCandidateOverflow(t *testing.T) {
	var code []byte
	for i := 0; i < 33; i++ {
		code = appendF64ConstForCacheTest(code, float64(i+1))
		code = append(code, 0x1a) // drop
	}
	// Were an incomplete 32-entry tally ranked, this would make 2 the winner.
	for i := 0; i < 40; i++ {
		code = appendF64ConstForCacheTest(code, 2)
		code = append(code, 0x1a)
	}
	code = append(code, 0x0b)

	f := &fn{a: &a64.Asm{}, s: newStack()}
	f.preloadFloatConsts(code)
	for i, want := range []float64{1, 2} {
		if got := uint64(f.fconsts[i].bits); got != math.Float64bits(want) {
			t.Fatalf("cached constant %d bits = %#x, want first-seen fallback %#x", i, got, math.Float64bits(want))
		}
	}
}
