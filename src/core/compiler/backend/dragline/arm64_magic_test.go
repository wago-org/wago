package dragline

import "testing"

func TestARM64UnsignedI32ImmediateMagic(t *testing.T) {
	for _, divisor := range []uint32{5, 6, 9, 10, 11, 12, 13, 25, 30, 67, 100, 125, 1_000_000, 100_000_000} {
		multiplier, shift, ok := arm64UnsignedI32ImmediateMagic(divisor)
		if !ok {
			continue
		}
		values := []uint32{0, 1, divisor - 1, divisor, divisor + 1, ^uint32(0) - 1, ^uint32(0)}
		state := uint32(0x9e3779b9) ^ divisor
		for range 100_000 {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			values = append(values, state)
		}
		for _, dividend := range values {
			got := uint32(uint64(dividend) * uint64(multiplier) >> shift)
			if want := dividend / divisor; got != want {
				t.Fatalf("%d / %d via %#x >> %d = %d, want %d", dividend, divisor, multiplier, shift, got, want)
			}
		}
	}
	if multiplier, shift, ok := arm64UnsignedI32ImmediateMagic(30); !ok || multiplier != 0x88888889 || shift != 36 {
		t.Fatalf("division by 30 magic = %#x >> %d, ok=%t", multiplier, shift, ok)
	}
}
