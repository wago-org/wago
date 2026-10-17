//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// A contiguous window of a:b is one PALIGNR with reversed source operands.
// Keep the general shuffle path for CPUs without SSSE3 and all other masks.
func (f *fn) tryV128AlignShuffle(a, b *elem, lanes [16]byte) bool {
	if lanes[0] > 16 {
		return false
	}
	for i, lane := range lanes {
		if lane != lanes[0]+byte(i) {
			return false
		}
	}
	if !f.cpuHas(shared.AMD64SSSE3) {
		return false
	}
	old := f.fpinned
	xa, aOwned := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb, bOwned := f.operandRegV128(b)
	f.fpinned = f.fpinned.add(xb)
	dst := f.allocFReg(maskOf(xa, xb))
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPAlignr(dst, xb, xa, lanes[0])
	} else {
		f.mov128(dst, xb)
		f.a.SseMapRRI(0x66, 0x3a, 0x0f, dst, xa, lanes[0])
	}
	f.fpinned = old
	if aOwned {
		f.releaseF(xa)
	}
	if bOwned {
		f.releaseF(xb)
	}
	f.stats.peep("simd-shuffle-align")
	f.pushVReg(dst)
	return true
}
