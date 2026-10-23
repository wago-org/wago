//go:build arm64

package arm64

import (
	"math"
	"os"
)

var floatLiteralPoolEnabled = os.Getenv("WAGO_ARM64_NO_FCONST_POOL") != "1"

type floatPoolConst struct {
	bits uint64
	head uint32 // floatPoolSites index + 1; zero ends the list
	f64  bool
}

type floatPoolSite struct {
	off  uint32
	next uint32
}

// floatBits returns the bit pattern of v in the given float width.
func floatBits(v float64, f64 bool) uint64 {
	if f64 {
		return math.Float64bits(v)
	}
	return uint64(math.Float32bits(float32(v)))
}

// Floating point (f32/f64), ported from WARP's aarch64 NEON lowering with the NaN-
// and signed-zero-correct sequences. Floats are handled eagerly: operands are
// materialized into NEON V registers by a parallel allocator (fregUser/fpinned) and
// the result is pushed as a V-resident value. V (NEON) and GP register namespaces
// are independent, so the integer condense engine is untouched.

// --- V (NEON) allocator ---

type floatConstReg struct {
	typ       machineType
	bits      int64
	reg       Reg
	loopDepth uint16
}

// IEEE-754 sign/magnitude masks for neg, abs, copysign.
const (
	fSignMask32 uint32 = 0x80000000
	fMagMask32  uint32 = 0x7FFFFFFF
	fSignMask64 uint64 = 0x8000000000000000
	fMagMask64  uint64 = 0x7FFFFFFFFFFFFFFF
)

// Rounding modes for FRINT* (nearest/floor/ceil/trunc), matching wasm's
// non-trapping rounding. The encoder maps these selectors to
// FRINTN/FRINTM/FRINTP/FRINTZ.
const (
	roundNearest byte = 'n'
	roundFloor   byte = 'm'
	roundCeil    byte = 'p'
	roundTrunc   byte = 'z'
)

// --- float op handlers ---

// --- saturating float→int truncation (0xFC 0-7): NaN→0, out-of-range clamps ---

// helpers

func fsize(f64 bool) int {
	if f64 {
		return 8
	}
	return 4
}
