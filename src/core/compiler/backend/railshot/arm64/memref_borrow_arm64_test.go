//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

// TestBorrowedLocalSurvivesDeferredCompareLoadArm64 is the minimized LZ4
// regression. A deferred comparison load addresses memory through local 0's
// pinned register while an older local.get 0 remains the outer add operand.
// Materializing the load must use a fresh register and retain the borrowed one.
func TestBorrowedLocalSurvivesDeferredCompareLoadArm64(t *testing.T) {
	body := []byte{0x01, 0x01, 0x7f} // one declared i32 local
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(100)...)
	body = append(body,
		0x21, 0x00, // local.set 0
		0x41,
	)
	body = append(body, wasmtest.SLEB32(100)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(5)...)
	body = append(body,
		0x3a, 0x00, 0x00, // i32.store8 mem[100] = 5
		0x41,
	)
	body = append(body, wasmtest.SLEB32(200)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(5)...)
	body = append(body,
		0x3a, 0x00, 0x00, // i32.store8 mem[200] = 5
		0x20, 0x00, // local.get 0: preserved outer add operand
		0x41,
	)
	body = append(body, wasmtest.SLEB32(200)...)
	body = append(body,
		0x2d, 0x00, 0x00, // i32.load8_u mem[200]
		0x20, 0x00,
		0x2d, 0x00, 0x00, // i32.load8_u mem[local 0], borrowing local 0's register
		0x46, // i32.eq
		0x6a, // i32.add: 100 + 1
		0x0b,
	)
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
	if got := runArm64(t, m); got != 101 {
		t.Fatalf("borrowed local after deferred comparison = %d, want 101", got)
	}
}
