package shared

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"testing"
)

func integerFloatProofBody(wide, clamp, unsigned bool, factor float64) []byte {
	b := []byte{0x40, 0x20, 1, 0x20, 2}
	if clamp {
		b = append(b, 0x41, 1, 0x6a)
	}
	convert, constant, multiply, sub := byte(0xb3), byte(0x43), byte(0x94), byte(0)
	if wide {
		convert, constant, multiply, sub = 0xb8, 0x44, 0xa2, 2
	}
	if !clamp {
		sub = 6
	}
	if unsigned {
		sub++
	}
	b = append(b, convert, constant)
	if wide {
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(factor))
	} else {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(float32(factor)))
	}
	b = append(b, multiply, 0xfc, sub)
	if !clamp {
		b = append(b, 0xa7)
	}
	return append(b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b)
}
func TestIntegerFloatSumProof(t *testing.T) {
	for _, c := range []struct {
		wide, clamp, unsigned          bool
		factor                         float64
		coefficient, divisor, constant uint64
	}{
		{true, false, false, 3.25, 3, 4, 0}, {true, false, true, 0.125, 0, 8, 0}, {true, false, false, 1.5, 1, 2, 0}, {true, false, false, 4, 4, 0, 0}, {true, false, false, 2097152, 2097152, 0, 0}, {true, false, false, 2147483648, 2147483648, 0, 0}, {true, false, true, 4294967296, 4294967296, 0, 0},
		{false, true, false, math.Ldexp(1, 31), 0, 0, 0x7fffffff}, {true, true, false, 1e10, 0, 0, 0x7fffffff}, {false, true, true, math.Ldexp(1, 32), 0, 0, 0xffffffff},
	} {
		b := integerFloatProofBody(c.wide, c.clamp, c.unsigned, c.factor)
		r := wasm.ReaderFrom(b)
		p, ok := InspectIntegerSumLoop(&r)
		if !ok || p.Wide || p.Counter != 2 || p.Coefficient != c.coefficient || p.Divisor != c.divisor || p.Constant != c.constant || r.Offset() != 0 {
			t.Fatalf("case=%+v plan=%+v accepted=%t", c, p, ok)
		}
		for cut := range len(b) {
			r = wasm.ReaderFrom(b[:cut])
			if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
				t.Fatalf("accepted cut=%d", cut)
			}
		}
	}
}
func TestIntegerFloatSumProofRejectsRoundingEffectsAndRange(t *testing.T) {
	b := integerFloatProofBody(true, false, false, 3.25)
	cases := map[string][]byte{
		"float32-rounding": integerFloatProofBody(false, false, false, 1.5), "nonunit-numerator": integerFloatProofBody(true, false, false, 3.75), "too-wide-product": integerFloatProofBody(true, false, false, 2097153), "negative": integerFloatProofBody(true, false, false, -3.25), "nan": integerFloatProofBody(true, false, false, math.NaN()), "infinity": integerFloatProofBody(true, false, false, math.Inf(1)), "clamp-too-small": integerFloatProofBody(false, true, false, 1.5),
		"trap": bytes.Replace(b, []byte{0xfc, 6}, []byte{0xb0}, 1), "signed-input": bytes.Replace(b, []byte{0xb8}, []byte{0xb7}, 1), "call": bytes.Replace(b, []byte{0xa2}, []byte{0x10, 0, 0xa2}, 1), "counter-write": bytes.Replace(b, []byte{0xb8}, []byte{0x22, 2, 0xb8}, 1), "other-counter": bytes.Replace(b, []byte{0x20, 2, 0xb8}, []byte{0x20, 0, 0xb8}, 1), "step": bytes.Replace(b, []byte{0x41, 1, 0x6a}, []byte{0x41, 2, 0x6a}, 1), "signed-bound": bytes.Replace(b, []byte{0x49}, []byte{0x48}, 1),
	}
	for name, b := range cases {
		r := wasm.ReaderFrom(b)
		if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s", name)
		}
	}
}
