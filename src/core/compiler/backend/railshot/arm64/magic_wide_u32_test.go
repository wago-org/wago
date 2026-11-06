//go:build arm64

package arm64

import (
	"math/bits"
	"testing"
)

func TestWideReciprocalU32ExactQuotients(t *testing.T) {
	check := func(n, d uint32) {
		t.Helper()
		hi, _ := bits.Mul64(uint64(n), wideReciprocalU32(uint64(d)))
		if hi != uint64(n/d) {
			t.Fatalf("n=%x d=%x high product=%x want=%x", n, d, hi, n/d)
		}
	}
	for _, d := range []uint32{3, 7, 19, 31, 97, 127, 65537, 0x7fffffff, 0x80000001, 0xffffffff} {
		for n := uint32(0); n < 1<<16; n++ {
			check(n, d)
		}
		q := uint64(^uint32(0)) / uint64(d)
		for _, center := range []uint64{uint64(d), q * uint64(d), uint64(1) << 31, uint64(^uint32(0))} {
			for offset := int64(-2); offset <= 2; offset++ {
				n := int64(center) + offset
				if n >= 0 && n <= int64(^uint32(0)) {
					check(uint32(n), d)
				}
			}
		}
	}
	state := uint64(0x91e10da5a5b63f27)
	for range 32768 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		d := uint32(state>>32) | 3
		check(uint32(state), d)
		check(^uint32(0), d)
		check(d-1, d)
		check(d, d)
	}
}
