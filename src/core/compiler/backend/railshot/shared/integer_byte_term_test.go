package shared

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func byteSumProofBody(index byte) []byte {
	body := []byte{0x40, 0x20, 1, 0x20, 2, 0xfd, 15, 0xfd, 12}
	body = append(body, bytes.Repeat([]byte{index}, 16)...)
	return append(body, 0xfd, 14, 0xfd, 22, 3, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b)
}

func TestIntegerByteProofIndices(t *testing.T) {
	for _, index := range []byte{0, 3, 15, 16, 127, 128, 255} {
		body := byteSumProofBody(index)
		r := wasm.ReaderFrom(body)
		p, ok := InspectIntegerSumLoop(&r)
		wantByte := index < 16
		wantCoefficient := uint64(0)
		if wantByte {
			wantCoefficient = 1
		}
		if !ok || p.UnsignedByte != wantByte || p.Coefficient != wantCoefficient || p.Constant != 0 || p.Counter != 2 || p.Wide || r.Offset() != 0 {
			t.Fatalf("index=%d: %+v accepted=%t cursor=%d", index, p, ok, r.Offset())
		}
		for cut := 0; cut < len(body); cut++ {
			r = wasm.ReaderFrom(body[:cut])
			if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
				t.Fatalf("index=%d truncated=%d accepted or advanced source", index, cut)
			}
		}
	}
}

func TestIntegerByteProofRejectsOtherExpressions(t *testing.T) {
	body := byteSumProofBody(15)
	constantMask := append([]byte{0xfd, 12}, bytes.Repeat([]byte{15}, 16)...)
	for name, changed := range map[string][]byte{
		"dynamic-mask":   bytes.Replace(body, constantMask, []byte{0x20, 3}, 1),
		"other-splat":    bytes.Replace(body, []byte{0xfd, 15}, []byte{0xfd, 16}, 1),
		"signed-extract": bytes.Replace(body, []byte{0xfd, 22, 3}, []byte{0xfd, 21, 3}, 1),
		"bad-lane":       bytes.Replace(body, []byte{0xfd, 22, 3}, []byte{0xfd, 22, 16}, 1),
		"relaxed-swizzle": bytes.Replace(body, []byte{0xfd, 14},
			[]byte{0xfd, 0x80, 2}, 1),
		"call":          bytes.Replace(body, []byte{0x20, 2, 0xfd, 15}, []byte{0x20, 2, 0x10, 0, 0xfd, 15}, 1),
		"counter-write": bytes.Replace(body, []byte{0x20, 2, 0xfd, 15}, []byte{0x20, 2, 0x22, 2, 0xfd, 15}, 1),
		"other-counter": bytes.Replace(body, []byte{0x20, 2, 0xfd, 15}, []byte{0x20, 3, 0xfd, 15}, 1),
		"memory":        bytes.Replace(body, []byte{0x20, 2, 0xfd, 15}, []byte{0x41, 0, 0x28, 2, 0, 0xfd, 15}, 1),
		"other-step":    bytes.Replace(body, []byte{0x41, 1, 0x6a}, []byte{0x41, 2, 0x6a}, 1),
		"signed-bound":  bytes.Replace(body, []byte{0x49}, []byte{0x48}, 1),
	} {
		r := wasm.ReaderFrom(changed)
		if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s or advanced source", name)
		}
	}
}
