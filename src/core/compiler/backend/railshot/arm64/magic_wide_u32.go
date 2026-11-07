//go:build arm64

package arm64

import "os"

var wideQuotientSinkEnabled = os.Getenv("WAGO_ARM64_NO_WIDE_QUOTIENT_SINK") != "1"

// For 1<d<2^32, m=ceil(2^64/d) gives floor(n*m/2^64)=floor(n/d)
// for every 0<=n<2^32. Write d*m=2^64+e, with 0<=e<d. The
// rounding error n*e/(d*2^64) is less than 1/d because n*e<2^64,
// while n/d is at least 1/d below the next integer. Rounding cannot
// cross that quotient boundary. Callers exclude zero and one.
func wideReciprocalU32(divisor uint64) uint64 {
	return ^uint64(0)/divisor + 1
}

// magicWideU32Quotient returns a pinned quotient register. The W move is
// required even for a borrowed i32 local: its physical carrier can have dirty
// upper bits, whereas UMULH consumes all 64 bits of both inputs.
func (f *fn) magicWideU32Quotient(dividend Reg, divisor uint64) Reg {
	return f.magicWideU32QuotientInto(dividend, divisor, regNone, false)
}

// A proven canonical dividend can feed UMULH directly. Unknown incoming i32
// carriers still need a W write before the 64-bit multiply.
func (f *fn) magicWideU32QuotientInto(dividend Reg, divisor uint64, dest Reg, clean bool) Reg {
	magic := wideReciprocalU32(divisor)
	mr, ownMr := f.readWideReciprocal(magic, maskOf(dividend, dest))
	mrWasPinned := f.pinned.has(mr)
	f.pinned = f.pinned.add(mr)
	if !ownMr {
		f.stats.peep("magic-loop-int-const")
	}
	q := dest
	if q == regNone {
		q = f.allocReg(maskOf(dividend, mr))
	}
	f.pinned = f.pinned.add(q)
	input := dividend
	if !clean {
		f.a.MovReg32(q, dividend)
		input = q
	}
	f.a.Umulh(q, input, mr)
	if !mrWasPinned {
		f.pinned = f.pinned.remove(mr)
	}
	if ownMr {
		f.release(mr)
	}
	f.stats.peep("magic-u32-wide-reciprocal")
	return q
}
