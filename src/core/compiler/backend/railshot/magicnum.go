// Package railshot is the architecture-NEUTRAL core of the railshot single-pass
// wasm backend. It holds the valent-block operand-stack model, the on-the-fly
// register allocator, the scanBody hint pre-scan, control-flow reconciliation
// state, and target-independent helpers (constant folding, magic-number division
// derivation). The architecture-specific instruction selection + encoders live in
// the sibling packages railshot/amd64 and railshot/arm64, which import this core.
//
// See the arm64-port plan for the extraction in progress.
package railshot

import (
	"math/bits"
)

// Magic-number derivation for constant division. This is the libdivide /
// Granlund–Montgomery construction; it runs once per div-by-const at compile
// time. The largest numerator is 2^127 and the proposed quotient is always
// narrower than the divisor's W-bit domain, so one bits.Div64 computes the
// exact 128-by-64 quotient without heap-backed big integers.

// MagicU returns (magic, shift, add) for unsigned W-bit division by d, where d is
// not a power of two and 2 <= d < 2^W. The quotient of n is:
//
//	q = MULHU(magic, n)
//	if add: q = ((n - q) >> 1) + q
//	q >>= shift
func MagicU(d uint64, W uint) (magic uint64, shift uint, add bool) {
	fl := uint(bits.Len64(d)) - 1 // floor(log2 d)
	// proposed = floor(2^(W+fl) / d), rem = 2^(W+fl) mod d.
	pm, rem := divPow2By64(W+fl, d)
	e := d - rem
	if e < uint64(1)<<fl {
		pm++ // magic fits in W bits, no add correction
		return truncW(pm, W), fl, false
	}
	pm *= 2 // low W bits of 2*proposed
	if rem >= d-rem {
		pm++
	}
	pm++
	return truncW(pm, W), fl, true
}

// MagicS returns (magic, shift, addN) for signed W-bit division by the positive
// magnitude ad (2 <= ad < 2^(W-1), not a power of two). The quotient of n is:
//
//	q = MULHS(magic, n)   // signed high half
//	if addN: q += n
//	q >>= shift           // arithmetic
//	q += (unsigned)q >> (W-1)
//
// magic is returned as its signed W-bit reinterpretation (may be negative).
func MagicS(ad uint64, W uint) (magic int64, shift uint, addN bool) {
	fl := uint(bits.Len64(ad)) - 1 // floor(log2 ad)
	// proposed = floor(2^(W-1+fl) / ad), rem = 2^(W-1+fl) mod ad.
	pm, rem := divPow2By64(W-1+fl, ad)
	e := ad - rem
	if e < uint64(1)<<fl {
		pm++
		return signW(truncW(pm, W), W), fl - 1, false
	}
	pm *= 2
	if rem >= ad-rem {
		pm++
	}
	pm++
	return signW(truncW(pm, W), W), fl, true
}

// divPow2By64 returns the quotient and remainder of 2^exp / d. MagicU and
// MagicS guarantee exp <= 127 and that the quotient fits in 64 bits.
func divPow2By64(exp uint, d uint64) (q, rem uint64) {
	if exp < 64 {
		return bits.Div64(0, uint64(1)<<exp, d)
	}
	return bits.Div64(uint64(1)<<(exp-64), 0, d)
}

// truncW returns the low W bits of v as a uint64.
func truncW(v uint64, W uint) uint64 {
	if W >= 64 {
		return v
	}
	return v & ((uint64(1) << W) - 1)
}

// signW reinterprets the low W bits of m as a signed W-bit value.
func signW(m uint64, W uint) int64 {
	if W >= 64 {
		return int64(m)
	}
	if m&(uint64(1)<<(W-1)) != 0 {
		return int64(m) - (int64(1) << W)
	}
	return int64(m)
}
