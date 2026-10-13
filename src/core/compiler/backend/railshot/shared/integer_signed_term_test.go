package shared

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIntegerSignedSumProofRejectsEffectsAndMalformedSuffixes(t *testing.T) {
	body := []byte{0x40, 0x20, 1, 0x20, 2, 0x41, 13, 0x6c, 0xad, 0xc4, 0x42, 32, 0x88, 0xa7, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	r := wasm.ReaderFrom(body)
	p, ok := InspectIntegerSumLoop(&r)
	if !ok || p.SignedBits != 32 || !p.NegativeCount || p.Coefficient != 13 || p.Counter != 2 || r.Offset() != 0 {
		t.Fatalf("plan=%+v accepted=%t cursor=%d", p, ok, r.Offset())
	}
	for cut := 0; cut < len(body); cut++ {
		r = wasm.ReaderFrom(body[:cut])
		if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted truncated body at %d or advanced source", cut)
		}
	}
	for name, changed := range map[string][]byte{
		"other-shift":     bytes.Replace(body, []byte{0x42, 32}, []byte{0x42, 31}, 1),
		"other-wrap":      bytes.Replace(body, []byte{0xa7}, []byte{0xa8}, 1),
		"other-counter":   bytes.Replace(body, []byte{0x20, 2, 0x41, 13}, []byte{0x20, 0, 0x41, 13}, 1),
		"nonlinear":       bytes.Replace(body, []byte{0x41, 13}, []byte{0x20, 2}, 1),
		"memory":          bytes.Replace(body, []byte{0x20, 2, 0x41, 13, 0x6c}, []byte{0x41, 0, 0x28, 2, 0}, 1),
		"call":            bytes.Replace(body, []byte{0x20, 2, 0x41, 13, 0x6c}, []byte{0x20, 2, 0x10, 0}, 1),
		"global":          bytes.Replace(body, []byte{0x20, 2, 0x41, 13, 0x6c}, []byte{0x23, 0}, 1),
		"division":        bytes.Replace(body, []byte{0x6c}, []byte{0x6d}, 1),
		"counter-write":   bytes.Replace(body, []byte{0x6c}, []byte{0x6c, 0x22, 2}, 1),
		"nested-control":  bytes.Replace(body, []byte{0x20, 2, 0x41, 13, 0x6c}, []byte{0x02, 0x7f, 0x20, 2, 0x0b}, 1),
		"signed-bound":    bytes.Replace(body, []byte{0x49}, []byte{0x48}, 1),
		"different-step":  bytes.Replace(body, []byte{0x41, 1, 0x6a}, []byte{0x41, 2, 0x6a}, 1),
		"different-label": bytes.Replace(body, []byte{0x0d, 0}, []byte{0x0d, 1}, 1),
	} {
		r = wasm.ReaderFrom(changed)
		if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s or advanced source", name)
		}
	}
}
