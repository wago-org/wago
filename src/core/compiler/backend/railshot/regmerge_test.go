//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestRegMergeBlockResult exercises the phase-2 register-merge path: a block with
// a single i32 result reached by a br_if edge (value carried) and a fall-through
// edge must produce identical, correct results with the reg-merge path on and off.
//
//	(func (param x i32) (param c i32) (result i32)
//	  (block (result i32)
//	    (local.get x)
//	    (br_if 0 (local.get c))   ;; taken: block result = x
//	    (drop) (i32.const 999)))  ;; fall-through: result = 999
func TestRegMergeBlockResult(t *testing.T) {
	body := []byte{
		0x00,       // 0 local groups
		0x02, 0x7F, // block (result i32)
		0x20, 0x00, // local.get 0 (x)
		0x20, 0x01, // local.get 1 (c)
		0x0D, 0x00, // br_if 0
		0x1A,             // drop
		0x41, 0xE7, 0x07, // i32.const 999
		0x0B, // end block
		0x0B, // end func
	}
	m := mod1(t, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, body)
	cases := []struct{ x, c, want int32 }{{5, 1, 5}, {5, 0, 999}, {-3, 42, -3}, {7, 0, 999}}

	saved := regMergeEnabled
	defer func() { regMergeEnabled = saved }()
	for _, on := range []bool{false, true} {
		regMergeEnabled = on
		for _, tc := range cases {
			if got := runAmd64(t, m, tc.x, tc.c); got != tc.want {
				t.Errorf("regMerge=%v sel(%d,%d)=%d want %d", on, tc.x, tc.c, got, tc.want)
			}
		}
	}
}
