//go:build amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestExceptionRootMapsTypedReferenceBlockTypes(t *testing.T) {
	for _, opcode := range []byte{0x02, 0x03, 0x04, 0x1f} {
		for _, reference := range []byte{0x63, 0x64} {
			t.Run(fmt.Sprintf("op%x/ref%x", opcode, reference), func(t *testing.T) {
				types := [][]byte{wasmtest.FuncType(nil, nil)}
				for len(types) <= 106 {
					types = append(types, []byte{0x5f, 0x00})
				}
				var body []byte
				if opcode == 0x04 {
					body = append(body, 0x41, 0x01)
				}
				if opcode == 0x1f {
					body = append(body, 0x02, 0x69)
				} // outer exnref branch target
				body = append(body, opcode, reference, 0xea, 0x00)
				if opcode == 0x1f {
					body = append(body, 0x01, byte(wasm.CatchAllRef), 0x00)
				}
				if opcode == 0x04 {
					body = append(body, 0x00, 0x05)
				}
				body = append(body, 0x00, 0x0b, 0x1a)
				if opcode == 0x1f {
					body = append(body, 0x00, 0x0b, 0x1a)
				}
				body = append(body, 0x0b)
				m, err := wasm.DecodeModule(wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(types...)),
					wasmtest.Section(3, wasmtest.Vec([]byte{0})),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
				))
				if err != nil {
					t.Fatal(err)
				}
				if err := wasm.ValidateModule(m); err != nil {
					t.Fatalf("invalid fixture: %v", err)
				}
				maps, err := BuildExceptionRootMaps(m)
				if err != nil {
					t.Fatal(err)
				}
				if opcode != 0x1f {
					if len(maps) != 0 {
						t.Fatalf("unexpected exception roots: %v", maps)
					}
					return
				}
				if len(maps) != 1 || maps[0].LocalFunction != 0 || len(maps[0].Slots) != shared.EHPayloadLanes-shared.EHGCLaneBase {
					t.Fatalf("missing catch_all_ref roots: %+v", maps)
				}
				for _, slot := range maps[0].Slots {
					if slot.Kind != nativeabi.RootGCRef || slot.Offset >= maps[0].FrameBytes {
						t.Fatalf("invalid root slot: %+v", slot)
					}
				}
			})
		}
	}
}
