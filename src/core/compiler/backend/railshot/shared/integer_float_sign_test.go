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
