package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestIntegerSumLoopRecognizerRejectsEffectsAndChangedRecurrences(t *testing.T) {
	body := []byte{0x40, 0xfe, 3, 0, 0x20, 1, 0x02, 0x7f, 0xfe, 3, 0, 0xfe, 3, 0, 0x20, 2, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	r := wasm.ReaderFrom(body)
	p, ok := InspectIntegerSumLoop(&r)
	if !ok || !p.Fenced || p.Wide || p.Counter != 2 || p.Accumulator != 1 || p.Bound != 0 || p.End != len(body) || r.Offset() != 0 {
		t.Fatalf("plan=%+v ok=%t source=%d", p, ok, r.Offset())
	}
	for cut := 0; cut < len(body); cut++ {
		r = wasm.ReaderFrom(body[:cut])
		if _, ok := InspectIntegerSumLoop(&r); ok {
			t.Fatalf("accepted truncated body at %d", cut)
		}
		if r.Offset() != 0 {
			t.Fatal("failed match changed source cursor")
		}
	}
	for name, changed := range map[string][]byte{
		"memory-load":     bytes.Replace(body, []byte{0x20, 2, 0x0b}, []byte{0x41, 0, 0x28, 2, 0, 0x0b}, 1),
		"atomic-load":     bytes.Replace(body, []byte{0xfe, 3, 0}, []byte{0xfe, 0x10, 2, 0}, 1),
		"call":            bytes.Replace(body, []byte{0xfe, 3, 0}, []byte{0x10, 0}, 1),
		"trap":            bytes.Replace(body, []byte{0xfe, 3, 0}, []byte{0x00}, 1),
		"different-step":  bytes.Replace(body, []byte{0x41, 1}, []byte{0x41, 2}, 1),
		"signed-bound":    bytes.Replace(body, []byte{0x49}, []byte{0x48}, 1),
		"different-label": bytes.Replace(body, []byte{0x0d, 0}, []byte{0x0d, 1}, 1),
		"aliased-bound":   bytes.Replace(body, []byte{0x20, 0, 0x49}, []byte{0x20, 1, 0x49}, 1),
	} {
		r = wasm.ReaderFrom(changed)
		if _, ok := InspectIntegerSumLoop(&r); ok {
			t.Fatalf("accepted %s", name)
		}
	}
}
