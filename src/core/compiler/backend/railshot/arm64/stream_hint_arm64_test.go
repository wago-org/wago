//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestStreamingReductionDecodedHint(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"three-additions", []byte{0x41, 1, 0x41, 2, 0x41, 3, 0x41, 4, 0x6a, 0x6a, 0x6a, 0x1a, 0x0b}, true},
		{"constant-payload-and-two-additions", []byte{0x41, 1, 0x41, 2, 0x41, 0x6a, 0x6a, 0x6a, 0x1a, 0x0b}, false},
		{"floating-payload", []byte{0x43, 0x6a, 0x6a, 0x6a, 0, 0x1a, 0x0b}, false},
		{"nested-three-additions", []byte{0x02, 0x40, 0x41, 1, 0x41, 2, 0x41, 3, 0x41, 4, 0x6a, 0x6a, 0x6a, 0x1a, 0x0b, 0x0b}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := scanBodyBytes(tc.body, 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if h.hasStreamingReduction() != tc.want {
				t.Fatalf("hint = %v, want %v", h.hasStreamingReduction(), tc.want)
			}
			m := &wasm.Module{Code: []wasm.Func{{BodyBytes: tc.body}}}
			plain := h.funcHints
			plain.immediateFreeOps &^= streamingReductionHintMask
			if moduleStackArenaCap(m, []funcHints{h.funcHints}) != moduleStackArenaCap(m, []funcHints{plain}) {
				t.Fatal("reduction hint changed density sizing")
			}
		})
	}
	h := scanBody(wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Add}, {Kind: wasm.InstrI32Add}, {Kind: wasm.InstrI32Add}}}, 0, 0, 0)
	if !h.hasStreamingReduction() {
		t.Fatal("AST scanner missed add run")
	}
}
