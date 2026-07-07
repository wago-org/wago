package amd64

import (
	"math/big"
	"math/bits"
)

// Magic-number derivation for constant division, computed exactly with math/big
// (no fixed-width overflow to reason about). This is the libdivide / Granlund–
// Montgomery construction; it runs once per div-by-const at compile time.

// magicU returns (magic, shift, add) for unsigned W-bit division by d, where d is
// not a power of two and 2 <= d < 2^W. The quotient of n is:
//
//	q = MULHU(magic, n)
//	if add: q = ((n - q) >> 1) + q
//	q >>= shift
func magicU(d uint64, W uint) (magic uint64, shift uint, add bool) {
	one := big.NewInt(1)
	fl := uint(bits.Len64(d)) - 1 // floor(log2 d)
	D := new(big.Int).SetUint64(d)
	// proposed = floor(2^(W+fl) / d), rem = 2^(W+fl) mod d.
	num := new(big.Int).Lsh(one, W+fl)
	pm, rem := new(big.Int), new(big.Int)
	pm.DivMod(num, D, rem)
	e := new(big.Int).Sub(D, rem) // e = d - rem
	if e.Cmp(new(big.Int).Lsh(one, fl)) < 0 {
		pm.Add(pm, one) // magic fits in W bits, no add correction
		return truncW(pm, W), fl, false
	}
	pm.Lsh(pm, 1) // 2*proposed
	if new(big.Int).Lsh(rem, 1).Cmp(D) >= 0 {
		pm.Add(pm, one)
	}
	pm.Add(pm, one)
	return truncW(pm, W), fl, true
}

// magicS returns (magic, shift, addN) for signed W-bit division by the positive
// magnitude ad (2 <= ad < 2^(W-1), not a power of two). The quotient of n is:
//
//	q = MULHS(magic, n)   // signed high half
//	if addN: q += n
//	q >>= shift           // arithmetic
//	q += (unsigned)q >> (W-1)
//
// magic is returned as its signed W-bit reinterpretation (may be negative).
func magicS(ad uint64, W uint) (magic int64, shift uint, addN bool) {
	one := big.NewInt(1)
	fl := uint(bits.Len64(ad)) - 1 // floor(log2 ad)
	D := new(big.Int).SetUint64(ad)
	// proposed = floor(2^(W-1+fl) / ad), rem = 2^(W-1+fl) mod ad.
	num := new(big.Int).Lsh(one, W-1+fl)
	pm, rem := new(big.Int), new(big.Int)
	pm.DivMod(num, D, rem)
	e := new(big.Int).Sub(D, rem)
	if e.Cmp(new(big.Int).Lsh(one, fl)) < 0 {
		pm.Add(pm, one)
		return signW(truncW(pm, W), W), fl - 1, false
	}
	pm.Lsh(pm, 1)
	if new(big.Int).Lsh(rem, 1).Cmp(D) >= 0 {
		pm.Add(pm, one)
	}
	pm.Add(pm, one)
	return signW(truncW(pm, W), W), fl, true
}

// truncW returns the low W bits of v as a uint64.
func truncW(v *big.Int, W uint) uint64 {
	if W >= 64 {
		return v.Uint64()
	}
	return v.Uint64() & ((uint64(1) << W) - 1)
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
