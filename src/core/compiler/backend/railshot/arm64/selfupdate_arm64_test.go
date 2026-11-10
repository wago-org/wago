//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// In-place local self-update (`local.set $x (binop (local.get $x) …)`) condenses
// straight into x's register. Ported from amd64/selfupdate_test.go: the tricky
// aliasing cases the sink relies on — the self-local as the RIGHT operand of a
// non-commutative op (must be read before dest is overwritten), and both operands
// being the same self-local.
func TestInPlaceSelfUpdateArm64(t *testing.T) {
	// x = 100 - x; return x  (x is the RHS of a non-commutative sub, aliasing dest)
	subRev := []byte{
		0x00,
		0x41, 0xE4, 0x00, // i32.const 100
		0x20, 0x00, // local.get 0
		0x6B,       // i32.sub → 100 - x
		0x21, 0x00, // local.set 0
		0x20, 0x00, // local.get 0
		0x0B,
	}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, subRev)
	for _, tc := range []struct{ x, want int32 }{{30, 70}, {0, 100}, {-5, 105}, {100, 0}} {
		if got := int32(uint32(runArm64u(t, m, uint64(uint32(tc.x))))); got != tc.want {
			t.Errorf("(100-x) x=%d = %d, want %d", tc.x, got, tc.want)
		}
	}

	// x = x - x; return x  (both operands the same self-local → always 0)
	subSelf := []byte{
		0x00,
		0x20, 0x00, 0x20, 0x00, 0x6B, 0x21, 0x00, 0x20, 0x00, 0x0B,
	}
	m2 := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, subSelf)
	for _, x := range []int32{7, -1, 0} {
		if got := int32(uint32(runArm64u(t, m2, uint64(uint32(x))))); got != 0 {
			t.Errorf("(x-x) x=%d = %d, want 0", x, got)
		}
	}
}
