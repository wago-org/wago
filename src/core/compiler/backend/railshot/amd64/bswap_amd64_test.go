//go:build linux && amd64

package amd64

import (
	"math/bits"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func byteSwapTeeBody(firstMask, firstRotate int32, secondLocal byte) []byte {
	b := []byte{0x00, 0x20, 0x00, 0x22, 0x00, 0x41} // local.get 0; local.tee 0; i32.const
	b = append(b, wasmtest.SLEB32(firstMask)...)
	b = append(b, 0x71, 0x41) // and; i32.const
	b = append(b, wasmtest.SLEB32(firstRotate)...)
	b = append(b, 0x78, 0x20, secondLocal, 0x41) // rotr; local.get; i32.const
	b = append(b, wasmtest.SLEB32(24)...)
	b = append(b, 0x78, 0x41) // rotr; i32.const
	b = append(b, wasmtest.SLEB32(0x00ff00ff)...)
	return append(b, 0x71, 0x72, 0x0b) // and; or; end
}

func TestByteSwapAfterTee(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32},
		byteSwapTeeBody(0x00ff00ff, 8, 0))
	stats := compileWithStats(t, m, false).Funcs[0]
	if got := stats.Peephole["i32-bswap-tee"]; got != 1 {
		t.Fatalf("i32-bswap-tee = %d, want 1", got)
	}
	if got := disasm(t, compileCode(t, m, false)); !strings.Contains(got, "bswap") {
		t.Fatalf("byte-swap cover did not emit BSWAP:\n%s", got)
	}
	for _, x := range []uint32{0, 1, 0x12345678, 0x80000000, 0xffffffff} {
		if got, want := uint32(runAmd64u(t, m, uint64(x))), bits.ReverseBytes32(x); got != want {
			t.Fatalf("x=%#x: got %#x, want %#x", x, got, want)
		}
	}
	body := byteSwapTeeBody(0x00ff00ff, 8, 0)
	body = append(body[:len(body)-1], 0x1a, 0x20, 0x00, 0x0b) // drop swap; read original local
	local := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	if got, want := uint32(runAmd64u(t, local, 0x12345678)), uint32(0x12345678); got != want {
		t.Fatalf("local after byte-swap: got %#x, want %#x", got, want)
	}
}

func TestByteSwapAfterTeeNearMisses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		firstMask   int32
		firstRotate int32
		secondLocal byte
	}{
		{"different mask", 0x00ff00fe, 8, 0},
		{"different rotate", 0x00ff00ff, 9, 0},
		{"different local", 0x00ff00ff, 8, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32},
				byteSwapTeeBody(tc.firstMask, tc.firstRotate, tc.secondLocal))
			stats := compileWithStats(t, m, false).Funcs[0]
			if got := stats.Peephole["i32-bswap-tee"]; got != 0 {
				t.Fatalf("near miss fused %d times", got)
			}
			x, y := uint32(0x12345678), uint32(0xfedcba98)
			second := x
			if tc.secondLocal == 1 {
				second = y
			}
			want := bits.RotateLeft32(x&uint32(tc.firstMask), -int(tc.firstRotate)) |
				bits.RotateLeft32(second, -24)&0x00ff00ff
			if got := uint32(runAmd64u(t, m, uint64(x), uint64(y))); got != want {
				t.Fatalf("got %#x, want %#x", got, want)
			}
		})
	}
}
