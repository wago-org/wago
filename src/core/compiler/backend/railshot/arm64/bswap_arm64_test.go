//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"math/bits"
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
	cm, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	code := cm.Code
	found := false
	for at := 0; at+4 <= len(code); at += 4 {
		if binary.LittleEndian.Uint32(code[at:])&0xfffffc00 == 0x5ac00800 {
			found = true
		}
	}
	if !found {
		t.Fatal("byte-swap cover did not emit REV")
	}
	for _, x := range []uint32{0, 1, 0x12345678, 0x80000000, 0xffffffff} {
		if got, want := uint32(runArm64u(t, m, uint64(x))), bits.ReverseBytes32(x); got != want {
			t.Fatalf("x=%#x: got %#x, want %#x", x, got, want)
		}
	}
	body := byteSwapTeeBody(0x00ff00ff, 8, 0)
	body = append(body[:len(body)-1], 0x1a, 0x20, 0x00, 0x0b) // drop swap; read original local
	local := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	if got, want := uint32(runArm64u(t, local, 0x12345678)), uint32(0x12345678); got != want {
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
			if got := uint32(runArm64u(t, m, uint64(x), uint64(y))); got != want {
				t.Fatalf("got %#x, want %#x", got, want)
			}
		})
	}
}

func TestByteSwapAfterTeeBooleanAndAblation(t *testing.T) {
	saved := byteSwapCoverEnabled
	defer func() { byteSwapCoverEnabled = saved }()
	for _, enabled := range []bool{false, true} {
		byteSwapCoverEnabled = enabled
		for _, compact := range []bool{false, true} {
			for _, original := range []bool{false, true} {
				body := byteSwapTeeBody(0x00ff00ff, 8, 0)
				// Produce a known boolean before the tee. Its swapped result is not a boolean.
				body = append(append([]byte{}, body[:3]...), append([]byte{0x45}, body[3:]...)...)
				if original {
					body = append(body[:len(body)-1], 0x1a, 0x20, 0, 0x0b)
				} else {
					body = append(body[:len(body)-1], 0x41, 0, 0x47, 0x0b) // compare swapped value with zero
				}
				m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
				for _, x := range []uint64{0, 1, 0x12345678, 0xffffffff00000000, ^uint64(0)} {
					want := uint64(0)
					if uint32(x) == 0 {
						want = 1
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: compact}, x)
					if err != nil || uint32(got) != uint32(want) {
						t.Fatalf("enabled=%v compact=%v original=%v x=%x got=%x want=%x err=%v", enabled, compact, original, x, got, want, err)
					}
				}
			}
		}
	}
}
