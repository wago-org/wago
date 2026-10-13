package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestAffineUnsignedNegativeSaturation(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		convert, neg, sat := byte(0xb3), byte(0x8c), byte(1)
		if f64 {
			convert, neg, sat = 0xb8, 0x9a, 3
		}
		body := []byte{0x20, 2, 0x20, 2, convert, neg, 0xfc, sat, 0x6a, 0x0b}
		r := wasm.ReaderFrom(body)
		got, ok := inspectIntegerAffineTerm(&r, true)
		if !ok || got.coefficient != 1 || got.constant != 0 || got.width != 4 || got.variable != 2 {
			t.Fatalf("f64=%t: %+v ok=%t", f64, got, ok)
		}
		for name, changed := range map[string][]byte{
			"positive":         {0x20, 2, convert, 0xfc, sat, 0x0b},
			"signed-convert":   {0x20, 2, convert - 1, neg, 0xfc, sat, 0x0b},
			"signed-saturate":  {0x20, 2, convert, neg, 0xfc, sat - 1, 0x0b},
			"integer-on-float": {0x20, 2, convert, 0x41, 1, 0x6a, 0x0b},
			"unfinished-float": {0x20, 2, convert, neg, 0x0b},
		} {
			r := wasm.ReaderFrom(changed)
			if _, ok := inspectIntegerAffineTerm(&r, true); ok {
				t.Fatalf("accepted %s f64=%t", name, f64)
			}
		}
	}
}

func TestAffineCopySignBitConstants(t *testing.T) {
	for _, negative := range []bool{false, true} {
		for _, bits := range []uint32{0, 0x80000000, 1, 0x3fc00000, 0x7f800000, 0x7fc12345} {
			body := []byte{0x43, byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24), 0x20, 2, 0x41, 1, 0x6a, 0xb3}
			if negative {
				body = append(body, 0x8c)
			}
			body = append(body, 0x98, 0xbc, 0x0b)
			r := wasm.ReaderFrom(body)
			got, ok := inspectIntegerAffineTerm(&r, true)
			expected := bits & 0x7fffffff
			if negative {
				expected |= 0x80000000
			}
			if !ok || got.coefficient != 0 || got.constant != uint64(expected) || got.width != 4 {
				t.Fatalf("bits=%x negative=%t: %+v ok=%t", bits, negative, got, ok)
			}
		}
	}
	r := wasm.ReaderFrom([]byte{0x43, 0, 0, 0, 0, 0x20, 2, 0xb3, 0x8c, 0x98, 0xbc, 0x41, 0, 0x46, 0x0b})
	got, ok := inspectIntegerAffineTerm(&r, true)
	if !ok || got.constant != 0 {
		t.Fatalf("constant eq: %+v ok=%t", got, ok)
	}
	for name, body := range map[string][]byte{
		"unknown-sign":      {0x43, 0, 0, 0, 0, 0x20, 2, 0xb2, 0x98, 0xbc, 0x0b},
		"unknown-magnitude": {0x20, 2, 0xb3, 0x20, 2, 0xb3, 0x98, 0xbc, 0x0b},
		"wrong-precision":   {0x43, 0, 0, 0, 0, 0x20, 2, 0xb8, 0x98, 0xbc, 0x0b},
		"wrong-reinterpret": {0x43, 0, 0, 0, 0, 0x20, 2, 0xb3, 0x98, 0xbd, 0x0b},
		"variable-eq":       {0x20, 2, 0x41, 0, 0x46, 0x0b},
	} {
		r := wasm.ReaderFrom(body)
		if _, ok := inspectIntegerAffineTerm(&r, true); ok {
			t.Fatalf("accepted %s", name)
		}
	}
}
