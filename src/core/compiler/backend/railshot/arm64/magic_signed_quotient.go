//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"os"
)

var signedQuotientDirectEnabled = os.Getenv("WAGO_ARM64_NO_SIGNED_QUOTIENT_DIRECT") != "1"

// Read the dividend once and keep the quotient in its own destination. With
// no add correction, one signed 64-bit shift performs both high-half extraction
// and the final shift. With a correction, preserve the intervening wrapping i32
// add. The shifted ADD then changes floor division to truncation toward zero.
func (f *fn) tryDirectSignedQuotient(node *elem, dest Reg, divisor int64) (Reg, bool) {
	if !f.opt(optSignedQuotientDirect) || divisor != int64(int32(divisor)) {
		return regNone, false
	}
	magnitude := uint64(divisor)
	if divisor < 0 {
		magnitude = uint64(-divisor)
	}
	if magnitude <= 1 || magnitude&(magnitude-1) == 0 {
		return regNone, false
	}
	previousPins := f.pinned
	dividend, owned := f.materializeRead(f.s.arg0(node))
	f.pinned = f.pinned.add(dividend)
	q := dest
	if q == regNone || q == dividend {
		q = f.allocReg(maskOf(dividend))
	}
	f.pinned = f.pinned.add(q)
	magic, shift, add := railshot.MagicS(magnitude, 32)
	constant, cached := f.cachedMagicConst(uint64(magic), false)
	if !cached {
		constant = q
		f.a.MovImm32(constant, int32(magic))
	} else {
		f.stats.peep("magic-loop-int-const")
	}
	f.a.Smull(q, dividend, constant)
	if add {
		f.a.AsrImm64(q, q, 32)
		f.a.Add32(q, q, dividend)
		if shift != 0 {
			f.a.AsrImm(q, q, uint8(shift), true)
		}
	} else {
		f.a.AsrImm64(q, q, uint8(32+shift))
	}
	f.a.AddShiftedReg(q, q, q, a64.RegShiftLSR, 31, true)
	if divisor < 0 {
		f.neg(q, false)
	}
	result := q
	if dest != regNone && dest != q {
		f.a.MovReg32(dest, q)
		f.pinned = f.pinned.remove(q)
		f.release(q)
		result = dest
	}
	f.pinned = previousPins
	if owned && dividend != result {
		f.release(dividend)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	f.stats.peep("signed-quotient-direct")
	f.stats.peep("div-by-const")
	return result, true
}
