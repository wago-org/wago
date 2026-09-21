// Package railshot is the architecture-NEUTRAL core of the railshot single-pass
// wasm backend. It holds the valent-block operand-stack model, the on-the-fly
// register allocator, the scanBody hint pre-scan, control-flow reconciliation
// state, and target-independent helpers (constant folding, magic-number division
// derivation). The architecture-specific instruction selection + encoders live in
// the sibling packages railshot/amd64 and railshot/arm64, which import this core.
//
// See the arm64-port plan for the extraction in progress.
package railshot

import "github.com/wago-org/wago/src/core/compiler/optimization"

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
	return optimization.MagicU(d, W)
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
	return optimization.MagicS(ad, W)
}

// signW reinterprets the low W bits of m as a signed W-bit value. The
// reference tests retain this local helper while production derivation lives
// in the compiler-neutral optimization package.
func signW(m uint64, W uint) int64 {
	if W >= 64 {
		return int64(m)
	}
	if m&(uint64(1)<<(W-1)) != 0 {
		return int64(m) - (int64(1) << W)
	}
	return int64(m)
}
