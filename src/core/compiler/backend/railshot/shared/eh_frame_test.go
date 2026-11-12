package shared

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestEHFrameShapeTypedReferenceBlockTypes(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	for _, opcode := range []byte{0x02, 0x03, 0x04, 0x1f} {
		for _, reference := range []byte{0x63, 0x64} {
			t.Run(fmt.Sprintf("op%x/ref%x", opcode, reference), func(t *testing.T) {
				// Type index 106 needs two signed LEB bytes. Its first byte is
				// not an opcode, so a partial block-type read fails visibly.
				body := []byte{opcode, reference, 0xea, 0x00}
				want := EHFrameShape{}
				if opcode == 0x1f {
					body = append(body, 1, byte(wasm.CatchAllRef), 0)
					want = EHFrameShape{TryRecords: 1, RootRecords: 1}
				}
				body = append(body, 0x00, 0x0b, 0x0b)
				got, err := ScanEHFrameShape(&classifier, body)
				if err != nil || got != want {
					t.Fatalf("shape = %+v, %v; want %+v", got, err, want)
				}
			})
		}
	}
}
