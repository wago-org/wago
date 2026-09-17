package dragline

// amd64UnsignedI32ImmediateMagic finds a positive signed-immediate multiplier
// for an exact unsigned division. For p = 32+s and m = ceil(2^p/d), the
// round-up construction is exact over every uint32 when m*d-2^p <= 2^s.
// Restricting m below 2^31 lets IMUL encode it directly without sign extension
// changing the product.
func amd64UnsignedI32ImmediateMagic(divisor uint32) (multiplier uint32, shift byte, ok bool) {
	if divisor < 2 {
		return 0, 0, false
	}
	for s := uint(0); s < 32; s++ {
		power := uint64(1) << (32 + s)
		m := (power + uint64(divisor) - 1) / uint64(divisor)
		if m >= 1<<31 {
			return 0, 0, false
		}
		if m*uint64(divisor)-power <= uint64(1)<<s {
			return uint32(m), byte(32 + s), true
		}
	}
	return 0, 0, false
}
