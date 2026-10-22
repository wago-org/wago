//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarFloatMinMaxKinds = []uint32{232, 233, 234, 235, 244, 245, 246, 247}

// The oracle uses IEEE bits and the independently tested comparison oracle;
// no host floating operation can quiet a selected signaling NaN payload.
func swarFloatMinMaxOracle(a, b uint64, width uint8, maximum, pseudo bool) uint64 {
	if pseudo {
		left, right := b, a
		if maximum {
			left, right = right, left
		}
		if swarFloatComparisonOracle(left, right, int(width), int(swarFloatLT)) {
			return b
		}
		return a
	}
	exponent, fraction, sign, nan := uint64(0x7f800000), uint64(0x7fffff), uint64(0x80000000), uint64(0x7fc00000)
	if width == 64 {
		exponent, fraction, sign, nan = 0x7ff0000000000000, 0xfffffffffffff, 0x8000000000000000, 0x7ff8000000000000
	}
	if a&exponent == exponent && a&fraction != 0 || b&exponent == exponent && b&fraction != 0 {
		return nan
	}
	if a&^sign == 0 && b&^sign == 0 {
		if maximum {
			return a & b
		}
		return a | b
	}
	if swarFloatComparisonOracle(a, b, int(width), int(swarFloatLT)) == maximum {
		return b
	}
	return a
}

func TestSWARFloatingMinMaxAdmissionAndMapping(t *testing.T) {
	for _, kind := range swarFloatMinMaxKinds {
		width, maximum, pseudo, ok := swarFloatMinMaxOpcode(kind)
		base, wantWidth := uint32(232), uint8(32)
		if kind >= 244 {
			base, wantWidth = 244, 64
		}
		if !ok || width != wantWidth || maximum != ((kind-base)%2 == 1) || pseudo != (kind-base >= 2) {
			t.Fatal("FP minmax mapping", kind, width, maximum, pseudo, ok)
		}
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid FP minmax", kind, err)
		}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{}); code != nil || err == nil {
			t.Fatal("FP minmax through isolated bridge", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.F32}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("FP minmax result class", kind, err)
		}
		for _, prefix := range [][]byte{nil, {0x20, 0}, {0x20, 0, 0x43, 0, 0, 0, 0}, {0x41, 0, 0x20, 1}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[4:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("FP minmax carrier/underflow", kind, err)
			}
		}
	}
	for _, kind := range []uint32{0, 231, 236, 243, 248, ^uint32(0)} {
		if _, _, _, ok := swarFloatMinMaxOpcode(kind); ok {
			t.Fatal("FP minmax neighbor", kind)
		}
	}
	for _, width := range []uint8{0, 16, 63, 65, 255} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarFloatMinMax(width, false, false)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid FP minmax emitted", width)
		}
		f.swarFloatMinMax(32, true, true)
		if len(f.a.B) != before {
			t.Fatal("sticky FP minmax emitted")
		}
	}
}

func TestSWARFloatingMinMaxOracleEdges(t *testing.T) {
	for _, width := range []uint8{32, 64} {
		sign, one, negativeOne, signaling, quiet, canonical := uint64(0x80000000), uint64(0x3f800000), uint64(0xbf800000), uint64(0x7f800001), uint64(0xffc0dead), uint64(0x7fc00000)
		if width == 64 {
			sign, one, negativeOne, signaling, quiet, canonical = 0x8000000000000000, 0x3ff0000000000000, 0xbff0000000000000, 0x7ff0000000000001, 0xfff80000deadbeef, 0x7ff8000000000000
		}
		for _, test := range []struct{ a, b, min, max, pmin, pmax uint64 }{
			{0, sign, sign, 0, 0, 0}, {sign, 0, sign, 0, sign, sign},
			{signaling, one, canonical, canonical, signaling, signaling},
			{one, quiet, canonical, canonical, one, one},
			{signaling, quiet, canonical, canonical, signaling, signaling},
			{one, negativeOne, negativeOne, one, negativeOne, one},
			{negativeOne, one, negativeOne, one, negativeOne, one},
		} {
			for i, want := range []uint64{test.min, test.max, test.pmin, test.pmax} {
				if got := swarFloatMinMaxOracle(test.a, test.b, width, i%2 == 1, i >= 2); got != want {
					t.Fatal("FP minmax IEEE edge", width, i, test, got)
				}
			}
		}
	}
}
