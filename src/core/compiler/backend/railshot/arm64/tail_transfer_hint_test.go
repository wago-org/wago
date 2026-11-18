//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTailTransferHintTracksDecodedInstructions(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"ordinary call", []byte{0x10, 0, 0x0b}, false},
		{"direct tail", []byte{0x12, 0, 0x0b}, true},
		{"indirect tail", []byte{0x13, 0, 0, 0x0b}, true},
		{"nested tail", []byte{0x02, 0x40, 0x12, 0, 0x0b, 0x0b}, true},
		{"integer payload", []byte{0x41, 0x12, 0x1a, 0x0b}, false},
		{"float payload", []byte{0x43, 0x12, 0x13, 0, 0, 0x1a, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := scanBodyBytes(tc.body, 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if h.hasTailTransfer() != tc.want {
				t.Fatalf("tail transfer = %v, want %v", h.hasTailTransfer(), tc.want)
			}
			plain := h.funcHints
			plain.immediateFreeOps &^= tailTransferHintMask
			if plain.immediateFreeOpCount() != h.immediateFreeOpCount() {
				t.Fatal("tail hint changed density")
			}
		})
	}
	for _, kind := range []wasm.InstrKind{wasm.InstrCall, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect} {
		h := scanBody(wasm.Expr{Instrs: []wasm.Instruction{{Kind: kind}}}, 0, 0, 0)
		if h.hasTailTransfer() != (kind != wasm.InstrCall) {
			t.Fatalf("AST tail hint for %v", kind)
		}
	}
}
