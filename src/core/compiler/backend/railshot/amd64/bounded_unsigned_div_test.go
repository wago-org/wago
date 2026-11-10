//go:build linux && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestBoundedUnsignedReciprocalProof(t *testing.T) {
	for _, max := range []uint32{1, 7, 15, 63, 255, 1023, 4095, 65535} {
		for _, d := range []uint32{3, 5, 7, 9, 10, 11, 13, 17, 31, 63, 127, 255, 256, 257, 65535, 65537, 0x7fffffff} {
			m, s, ok := boundedUnsignedReciprocal(max, d)
			if !ok {
				continue
			}
			for n := uint32(0); n <= max; n++ {
				if q := uint32((uint64(n) * uint64(m)) >> s); q != n/d {
					t.Fatalf("max=%d d=%d n=%d multiplier=%d shift=%d got=%d", max, d, n, m, s, q)
				}
			}
		}
	}
	for _, x := range [][2]uint32{{0, 3}, {65536, 3}, {255, 0}, {255, 1}, {255, 2}, {255, 4}, {255, 0x80000000}} {
		if _, _, ok := boundedUnsignedReciprocal(x[0], x[1]); ok {
			t.Fatal("unsupported", x)
		}
	}
}

func TestBoundedUnsignedDivNative(t *testing.T) {
	saved := boundedUnsignedDivEnabled
	defer func() { boundedUnsignedDivEnabled = saved }()
	for _, mask := range []uint32{255, 65535, 0x800000ff} {
		for _, d := range []uint32{3, 7, 31, 65537} {
			for _, rem := range []bool{false, true} {
				op := byte(0x6e)
				if rem {
					op = 0x70
				}
				for _, swap := range []bool{false, true} {
					b := []byte{0}
					get := []byte{0x20, 0}
					c := append([]byte{0x41}, wasmtest.SLEB32(int32(mask))...)
					if swap {
						b = append(b, c...)
						b = append(b, get...)
					} else {
						b = append(b, get...)
						b = append(b, c...)
					}
					b = append(b, 0x71, 0x41)
					b = append(b, wasmtest.SLEB32(int32(d))...)
					b = append(b, op, 0xb)
					for _, on := range []bool{false, true} {
						boundedUnsignedDivEnabled = on
						m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, b)
						call, done := divInvoker(t, m)
						// Exhaust the full admitted domain and inject nonzero host upper bits.
						limit := mask
						if limit > 65535 {
							limit = 1023
						}
						for n := uint32(0); n <= limit; n++ {
							input := uint64(n) | 0xfedcba9800000000
							want := (n & mask) / d
							if rem {
								want = (n & mask) % d
							}
							if got := call(input); uint32(got) != want {
								t.Fatalf("mask=%x divisor=%d rem=%v swap=%v on=%v n=%x got=%x want=%x", mask, d, rem, swap, on, n, got, want)
							}
						}
						for _, n := range []uint32{0xffffffff, 0x80000000, 0xffff0000, 0x12345678} {
							want := (n & mask) / d
							if rem {
								want = (n & mask) % d
							}
							if got := call(uint64(n)); uint32(got) != want {
								t.Fatal("high operand", mask, d, on, n, got, want)
							}
						}
						done()
					}
				}
			}
		}
	}
}

func TestBoundedUnsignedDivNestedFixedRegisters(t *testing.T) {
	saved := boundedUnsignedDivEnabled
	defer func() { boundedUnsignedDivEnabled = saved }()
	for _, mask := range []uint32{255, 65535} {
		for _, innerRem := range []bool{false, true} {
			for _, outerRem := range []bool{false, true} {
				inner, outer := byte(0x6e), byte(0x6e)
				if innerRem {
					inner = 0x70
				}
				if outerRem {
					outer = 0x70
				}
				b := []byte{0, 0x20, 0, 0x41, 13, inner, 0x41}
				b = append(b, wasmtest.SLEB32(int32(mask))...)
				b = append(b, 0x71, 0x41, 7, outer, 0x20, 0, 0x73, 0x0b)
				for _, on := range []bool{false, true} {
					boundedUnsignedDivEnabled = on
					call, done := divInvoker(t, mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, b))
					for _, n := range []uint32{0, 1, 6, 7, 13, 255, 65535, 65536, 0x7fffffff, 0x80000000, 0xffffffff} {
						a := n / 13
						if innerRem {
							a = n % 13
						}
						a &= mask
						q := a / 7
						if outerRem {
							q = a % 7
						}
						want := q ^ n
						if got := call(uint64(n) | 0xabcd123400000000); uint32(got) != want {
							t.Fatal(mask, innerRem, outerRem, on, n, got, want)
						}
					}
					done()
				}
			}
		}
	}
}
