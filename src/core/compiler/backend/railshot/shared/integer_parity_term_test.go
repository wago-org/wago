package shared

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIntegerParitySumRecognizerBoundaries(t *testing.T) {
	body := []byte{0x40, 0x20, 1, 0x20, 2, 0x41, 1, 0x71, 0x04, 0x7f, 0x20, 2, 0x41, 3, 0x6c, 0x05, 0x20, 2, 0x41, 7, 0x6a, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	r := wasm.ReaderFrom(body)
	p, ok := InspectIntegerSumLoop(&r)
	if !ok || !p.Periodic || p.Counter != 2 || p.OddCoefficient != 3 || p.OddConstant != 0 || p.EvenCoefficient != 1 || p.EvenConstant != 7 || r.Offset() != 0 {
		t.Fatalf("plan=%+v accepted=%t cursor=%d", p, ok, r.Offset())
	}
	for cut := 0; cut < len(body); cut++ {
		r = wasm.ReaderFrom(body[:cut])
		if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("truncated match at %d", cut)
		}
	}
	for name, changed := range map[string][]byte{
		"other-mask":      bytes.Replace(body, []byte{0x41, 1, 0x71}, []byte{0x41, 2, 0x71}, 1),
		"other-predicate": bytes.Replace(body, []byte{0x20, 2, 0x41, 1, 0x71}, []byte{0x20, 1, 0x41, 1, 0x71}, 1),
		"other-variable":  bytes.Replace(body, []byte{0x20, 2, 0x41, 3}, []byte{0x20, 0, 0x41, 3}, 1),
		"other-width":     bytes.Replace(body, []byte{0x04, 0x7f}, []byte{0x04, 0x7e}, 1),
		"memory":          bytes.Replace(body, []byte{0x20, 2, 0x41, 3, 0x6c}, []byte{0x41, 0, 0x28, 2, 0}, 1),
		"call":            bytes.Replace(body, []byte{0x20, 2, 0x41, 3, 0x6c}, []byte{0x20, 2, 0x10, 0}, 1),
		"global":          bytes.Replace(body, []byte{0x20, 2, 0x41, 3, 0x6c}, []byte{0x23, 0}, 1),
		"trap":            bytes.Replace(body, []byte{0x20, 2, 0x41, 3, 0x6c}, []byte{0x00, 0x41, 3}, 1),
		"write":           bytes.Replace(body, []byte{0x20, 2, 0x41, 3, 0x6c}, []byte{0x20, 2, 0x22, 2}, 1),
		"nested-control":  bytes.Replace(body, []byte{0x20, 2, 0x41, 3, 0x6c}, []byte{0x02, 0x7f, 0x20, 2, 0x0b}, 1),
	} {
		r = wasm.ReaderFrom(changed)
		if _, ok := InspectIntegerSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("accepted %s or advanced failed cursor", name)
		}
	}
}
