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

// amd64SignedI32ImmediateMagic computes the signed multiplier and post-multiply
// shift from Hacker's Delight, Figure 10-1. It covers positive divisors greater
// than one; the quotient correction is determined by the multiplier's sign.
func amd64SignedI32ImmediateMagic(divisor int32) (multiplier int32, shift byte, ok bool) {
	if divisor <= 1 {
		return 0, 0, false
	}
	ad := uint64(uint32(divisor))
	t := uint64(1) << 31
	anc := t - 1 - t%ad
	p := uint(31)
	q1, r1 := t/anc, t-t/anc*anc
	q2, r2 := t/ad, t-t/ad*ad
	for {
		p++
		q1, r1 = q1*2, r1*2
		if r1 >= anc {
			q1++
			r1 -= anc
		}
		q2, r2 = q2*2, r2*2
		if r2 >= ad {
			q2++
			r2 -= ad
		}
		delta := ad - r2
		if q1 > delta || q1 == delta && r1 != 0 {
			return int32(uint32(q2 + 1)), byte(p - 32), true
		}
	}
}
