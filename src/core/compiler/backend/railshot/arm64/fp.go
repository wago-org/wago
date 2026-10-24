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

// IEEE-754 sign/magnitude masks for scalar sign operations.
const (
	fSignMask32 uint32 = 0x80000000
	fMagMask32  uint32 = 0x7fffffff
	fSignMask64 uint64 = 0x8000000000000000
	fMagMask64  uint64 = 0x7fffffffffffffff
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

// fsignInto writes a local assignment directly with the native sign op,
// preserving signaling NaN payloads. The regular path uses a bitwise mask
// and produces an owned value.
func (f *fn) fsignInto(dst Reg, f64, neg bool) {
	src, owned := f.operandRegF(f.popValue())
	if dst == regNone {
		saved := f.fpinned
		f.fpinned = saved.add(src)
		m := f.allocFReg(0)
		f.fpinned = f.fpinned.add(m)
		if neg {
			f.loadFMask(m, fSignMask64, fSignMask32, f64)
		} else {
			f.loadFMask(m, fMagMask64, fMagMask32, f64)
		}
		f.fpinned = saved
		dst = src
		if !owned {
			dst = f.allocFReg(maskOf(src, m))
		}
		if neg {
			f.a.Eor16b(dst, src, m)
		} else {
			f.a.And16b(dst, src, m)
		}
		f.releaseF(m)
	} else {
		if neg {
			f.a.NeonFneg(dst, src, f64)
		} else {
			f.a.NeonFabs(dst, src, f64)
		}
		f.stats.peep("float-native-sign")
	}
	if owned && dst != src {
		f.releaseF(src)
	}
	f.pushFReg(dst, mtOf2(f64))
}

// --- saturating float→int truncation (0xFC 0-7): NaN→0, out-of-range clamps ---

// helpers

func fsize(f64 bool) int {
	if f64 {
		return 8
	}
	return 4
}
