package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestStraightBlockProofRejectsControlAndCalls(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	for _, body := range [][]byte{
		{0x01, 0x0b}, {0xfc, 11, 0, 0x0b}, {0x41, 1, 0x1a, 0x0b}, {0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x1a, 0x0b},
	} {
		r := wasm.ReaderFrom(body)
		if !IsStraightLineBlock(&r, classifier) || r.Offset() != 0 {
			t.Fatalf("rejected %x or changed reader", body)
		}
	}
	for _, body := range [][]byte{{}, {0x00, 0x0b}, {0x02, 0x40, 0x0b, 0x0b}, {0x03, 0x40, 0x0b, 0x0b}, {0x0c, 0, 0x0b}, {0x0d, 0, 0x0b}, {0x0f, 0x0b}, {0x10, 0, 0x0b}, {0x40, 0, 0x0b}, {0xfc, 10, 0, 0, 0x0b}, {0xfc, 15, 0, 0x0b}, {0xfc, 11}, {0xfb, 0, 0x0b}, {0xfe, 0, 0x0b}, {0x41}} {
		r := wasm.ReaderFrom(body)
		if IsStraightLineBlock(&r, classifier) || r.Offset() != 0 {
			t.Fatalf("accepted %x or changed reader", body)
		}
	}
	for _, length := range []int{32, 256} {
		body := make([]byte, length+1)
		for i := 0; i < length; i++ {
			body[i] = 1
		}
		body[length] = 0x0b
		r := wasm.ReaderFrom(body)
		if IsStraightLineBlock(&r, classifier) {
			t.Fatalf("accepted unbounded block length %d", length)
		}
	}
}
