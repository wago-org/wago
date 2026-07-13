//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// sleb128 encodes v as signed LEB128 (for i32/i64.const immediates).
func sleb128(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			out = append(out, b)
			return out
		}
		out = append(out, b|0x80)
	}
}

// TestMulConstThreeOp checks `local * const` (folded to three-operand IMUL) for a
// range of general constants that bypass the LEA/shift strength reductions, i32
// and i64, against Go.
func TestMulConstThreeOp(t *testing.T) {
	consts := []int64{7, 11, 100, -5, 1000000, 6, 12, -1000}
	xs := []int64{0, 1, 3, -3, 123456, -7, 0x1_0000_0001}
	for _, is64 := range []bool{false, true} {
		typ, constOp, mulOp, fold := wasm.I32, byte(0x41), byte(0x6c), func(v int64) uint64 { return uint64(uint32(v)) }
		wname := "i32"
		if is64 {
			typ, constOp, mulOp, fold = wasm.I64, 0x42, 0x7e, func(v int64) uint64 { return uint64(v) }
			wname = "i64"
		}
		for _, c := range consts {
			c := c
			t.Run(wname, func(t *testing.T) {
				body := append([]byte{0x00, 0x20, 0x00, constOp}, sleb128(c)...)
				body = append(body, mulOp, 0x0b)
				m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
				for _, x := range xs {
					got := fold(int64(runAmd64u(t, m, uint64(x))))
					want := fold(x * c)
					if got != want {
						t.Fatalf("%s: %d * %d = %d, want %d", wname, x, c, got, want)
					}
				}
			})
		}
	}
}
