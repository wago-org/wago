//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDroppedLiteralsPreserveControlAndLiveValues(t *testing.T) {
	// Keep an argument live below nested controls. Literal/drop pairs neither
	// disturb that prefix nor consume the separate drop that removes 99.
	body := []byte{0x00, 0x20, 0x00, 0x02, 0x40, 0x03, 0x40,
		0x41, 0xe3, 0x00, // i32.const 99
		0x41, 0x7f, 0x1a, // i32.const -1; drop
		0x42, 0x7f, 0x1a, // i64.const -1; drop
		0x43, 0x01, 0x00, 0x80, 0x7f, 0x1a, // f32 NaN; drop
		0x44, 0x01, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0x1a, // f64 NaN; drop
		0x1a, 0x0c, 0x01, // drop 99; branch out of the block
		0x0b, 0x0b, 0x41, 0x03, 0x6a, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	if got := runAmd64(t, m, 39); got != 42 {
		t.Fatalf("result = %d, want 42", got)
	}
}
