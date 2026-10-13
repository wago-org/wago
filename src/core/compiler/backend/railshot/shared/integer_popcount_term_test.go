package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestIntegerPopcountSumProofRejectsEffectsAndMalformedSuffixes(t *testing.T) {
	body := []byte{0x40, 0x20, 1, 0x20, 2, 0x41, 7, 0x77, 0x69, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	r := wasm.ReaderFrom(body)
	p, ok := InspectIntegerSumLoop(&r)
	if !ok || !p.PopulationCount || p.Counter != 2 || p.Wide || r.Offset() != 0 {
		t.Fatalf("plan=%+v accepted=%t cursor=%d", p, ok, r.Offset())
	}
	for cut := 0; cut < len(body); cut++ {
		r = wasm.ReaderFrom(body[:cut])
		if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted cut=%d or advanced source", cut)
		}
	}
	for name, changed := range map[string][]byte{
		"shift":              bytes.Replace(body, []byte{0x77}, []byte{0x74}, 1),
		"nonconstant":        bytes.Replace(body, []byte{0x41, 7}, []byte{0x20, 0}, 1),
		"width":              bytes.Replace(body, []byte{0x69}, []byte{0x7b}, 1),
		"call":               bytes.Replace(body, []byte{0x69}, []byte{0x10, 0, 0x69}, 1),
		"counter-write":      bytes.Replace(body, []byte{0x69}, []byte{0x22, 2, 0x69}, 1),
		"global":             bytes.Replace(body, []byte{0x20, 2, 0x41, 7}, []byte{0x23, 0, 0x41, 7}, 1),
		"memory":             bytes.Replace(body, []byte{0x20, 2, 0x41, 7}, []byte{0x41, 0, 0x28, 2, 0, 0x41, 7}, 1),
		"nested":             bytes.Replace(body, []byte{0x20, 2, 0x41, 7}, []byte{0x02, 0x7f, 0x20, 2, 0x0b, 0x41, 7}, 1),
		"different-counter":  bytes.Replace(body, []byte{0x20, 2, 0x41, 7}, []byte{0x20, 0, 0x41, 7}, 1),
		"signed-bound":       bytes.Replace(body, []byte{0x49}, []byte{0x48}, 1),
		"step":               bytes.Replace(body, []byte{0x41, 1, 0x6a}, []byte{0x41, 2, 0x6a}, 1),
		"too-many-rotations": bytes.Replace(body, []byte{0x41, 7, 0x77}, bytes.Repeat([]byte{0x41, 7, 0x77}, 5), 1),
	} {
		r = wasm.ReaderFrom(changed)
		if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s or advanced source", name)
		}
	}
}
