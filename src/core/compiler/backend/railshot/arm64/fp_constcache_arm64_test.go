//go:build arm64

package arm64

import (
	"encoding/binary"
	"math"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func appendF64ConstForCacheTest(code []byte, value float64) []byte {
	code = append(code, 0x44)
	return binary.LittleEndian.AppendUint64(code, math.Float64bits(value))
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
