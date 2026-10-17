package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDLoadExtractRejectsOtherConsumersAndInvalidLanes(t *testing.T) {
	for _, bytes := range [][]byte{{}, {0xfd}, {0xfd, 0x1b}, {0xfd, 0x1b, 4}, {0xfd, 0x15, 16}, {0xfd, 0x1d, 2}, {0x1a}, {0xfd, 0x4e}} {
		r := wasm.ReaderFrom(bytes)
		if _, ok := InspectSIMDLoadExtract(&r); ok {
			t.Fatalf("accepted %x", bytes)
		}
		if r.Offset() != 0 {
			t.Fatal("lookahead changed source")
		}
	}
	for _, sub := range []byte{21, 22, 24, 25, 27, 29, 31, 33} {
		r := wasm.ReaderFrom([]byte{0xfd, sub, 0})
		p, ok := InspectSIMDLoadExtract(&r)
		if !ok || p.End != 3 || p.Offset != 0 || r.Offset() != 0 {
			t.Fatalf("sub=%d plan=%+v ok=%t", sub, p, ok)
		}
	}
}
