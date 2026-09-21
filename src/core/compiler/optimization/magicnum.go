package optimization

import "math/bits"

// MagicU returns (magic, shift, add) for unsigned W-bit division by d, where d
// is not a power of two and 2 <= d < 2^W.
func MagicU(d uint64, width uint) (magic uint64, shift uint, add bool) {
	fl := uint(bits.Len64(d)) - 1
	proposed, remainder := divPow2By64(width+fl, d)
	if d-remainder < uint64(1)<<fl {
		proposed++
		return truncateWidth(proposed, width), fl, false
	}
	proposed *= 2
	if remainder >= d-remainder {
		proposed++
	}
	proposed++
	return truncateWidth(proposed, width), fl, true
}

// MagicS returns (magic, shift, addDividend) for signed W-bit division by the
// positive magnitude d (2 <= d < 2^(W-1), not a power of two).
func MagicS(d uint64, width uint) (magic int64, shift uint, addDividend bool) {
	fl := uint(bits.Len64(d)) - 1
	proposed, remainder := divPow2By64(width-1+fl, d)
	if d-remainder < uint64(1)<<fl {
		proposed++
		return signWidth(truncateWidth(proposed, width), width), fl - 1, false
	}
	proposed *= 2
	if remainder >= d-remainder {
		proposed++
	}
	proposed++
	return signWidth(truncateWidth(proposed, width), width), fl, true
}

func divPow2By64(exp uint, divisor uint64) (quotient, remainder uint64) {
	if exp < 64 {
		return bits.Div64(0, uint64(1)<<exp, divisor)
	}
	return bits.Div64(uint64(1)<<(exp-64), 0, divisor)
}

func truncateWidth(value uint64, width uint) uint64 {
	if width >= 64 {
		return value
	}
	return value & (uint64(1)<<width - 1)
}

func signWidth(value uint64, width uint) int64 {
	if width >= 64 {
		return int64(value)
	}
	if value&(uint64(1)<<(width-1)) != 0 {
		return int64(value) - (int64(1) << width)
	}
	return int64(value)
}
