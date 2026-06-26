package amd64

import "math"

// Saturating float->int truncation (the 0xFC 0-7 ops: i32/i64.trunc_sat_f32/f64_s/u).
// Unlike the trapping trunc, out-of-range and NaN inputs saturate instead of
// trapping: NaN -> 0, values >= the high bound -> INT/UINT_MAX, values below the
// low bound -> INT_MIN / 0.
//
// cvtt truncates toward zero and yields the "integer indefinite" value
// (INT_MIN) for NaN and out-of-range inputs, which we fix up with float bound
// comparisons.

// floatBits returns the bit pattern of v in the source float width.
func floatBits(v float64, f64 bool) uint64 {
	if f64 {
		return math.Float64bits(v)
	}
	return uint64(math.Float32bits(float32(v)))
}

// loadFConst materializes a float constant (given its bits) into a fresh XMM.
func (g *cg) loadFConst(bits uint64, f64 bool) Reg {
	xmm := g.allocFReg()
	if f64 {
		g.a.MovImm64(RSI, bits)
		g.a.MovGprToXmm(xmm, RSI, true)
	} else {
		g.a.MovImm32(RSI, int32(uint32(bits)))
		g.a.MovGprToXmm(xmm, RSI, false)
	}
	return xmm
}

func (g *cg) truncSat(f64src, dstWide, signed bool) {
	x := g.materializeF(g.pop())
	r := g.allocReg()
	switch {
	case signed:
		g.truncSatSigned(x, r, f64src, dstWide)
	case dstWide:
		g.truncSatU64(x, r, f64src)
	default:
		g.truncSatU32(x, r, f64src)
	}
	g.freeFReg(x)
	g.pushReg(r)
}

// truncSatSigned: cvtt is correct for in-range inputs and for x below the low
// bound (both give INT_MIN); only NaN (-> 0) and x >= the high bound (-> MAX)
// need fixing.
func (g *cg) truncSatSigned(x, r Reg, f64src, dstWide bool) {
	n := 32
	if dstWide {
		n = 64
	}
	g.a.Cvttf2si(r, x, f64src, dstWide)

	g.a.Ucomis(x, x, f64src) // NaN?
	notNaN := g.a.JccPlaceholder(CondNP)
	g.a.XorSelf32(r) // NaN -> 0
	toEnd := g.a.JmpPlaceholder()
	g.a.PatchRel32(notNaN, g.a.Len())

	hi := g.loadFConst(floatBits(math.Ldexp(1, n-1), f64src), f64src) // 2^(n-1)
	g.a.Ucomis(x, hi, f64src)
	g.freeFReg(hi)
	below := g.a.JccPlaceholder(CondB) // x < 2^(n-1): cvtt result is correct
	if dstWide {
		g.a.MovImm64(r, 0x7FFFFFFFFFFFFFFF)
	} else {
		g.a.MovImm32(r, 0x7FFFFFFF)
	}
	g.a.PatchRel32(below, g.a.Len())
	g.a.PatchRel32(toEnd, g.a.Len())
}

// truncSatU32: convert via a 64-bit signed cvtt (exact for [0, 2^32)), then
// clamp: NaN/x<=0 -> 0, x >= 2^32 -> 0xFFFFFFFF.
func (g *cg) truncSatU32(x, r Reg, f64src bool) {
	g.a.Cvttf2si(r, x, f64src, true) // i64 trunc; low 32 is the u32 result in range

	zero := g.loadFConst(floatBits(0, f64src), f64src)
	g.a.Ucomis(x, zero, f64src)
	g.freeFReg(zero)
	pos := g.a.JccPlaceholder(CondA) // x > 0 (ordered); NaN/<=0 fall through to 0
	g.a.XorSelf32(r)
	toEnd := g.a.JmpPlaceholder()
	g.a.PatchRel32(pos, g.a.Len())

	hi := g.loadFConst(floatBits(math.Ldexp(1, 32), f64src), f64src) // 2^32
	g.a.Ucomis(x, hi, f64src)
	g.freeFReg(hi)
	below := g.a.JccPlaceholder(CondB) // x < 2^32: cvtt result is correct
	g.a.MovImm32(r, -1)                // 0xFFFFFFFF
	g.a.PatchRel32(below, g.a.Len())
	g.a.PatchRel32(toEnd, g.a.Len())
}

// truncSatU64: clamp (NaN/x<=0 -> 0, x >= 2^64 -> all ones), then convert. For
// x >= 2^63 a signed cvtt overflows, so bias by 2^63: cvtt(x - 2^63) + 2^63.
func (g *cg) truncSatU64(x, r Reg, f64src bool) {
	zero := g.loadFConst(floatBits(0, f64src), f64src)
	g.a.Ucomis(x, zero, f64src)
	g.freeFReg(zero)
	pos := g.a.JccPlaceholder(CondA) // x > 0; NaN/<=0 -> 0
	g.a.XorSelf32(r)
	end0 := g.a.JmpPlaceholder()
	g.a.PatchRel32(pos, g.a.Len())

	hi := g.loadFConst(floatBits(math.Ldexp(1, 64), f64src), f64src) // 2^64
	g.a.Ucomis(x, hi, f64src)
	g.freeFReg(hi)
	inRange := g.a.JccPlaceholder(CondB)
	g.a.MovImm64(r, 0xFFFFFFFFFFFFFFFF) // x >= 2^64 -> max
	endMax := g.a.JmpPlaceholder()
	g.a.PatchRel32(inRange, g.a.Len())

	p63 := g.loadFConst(floatBits(math.Ldexp(1, 63), f64src), f64src) // 2^63
	g.a.Ucomis(x, p63, f64src)
	simple := g.a.JccPlaceholder(CondB) // x < 2^63: direct cvtt
	g.a.FSub(x, p63, f64src)            // x -= 2^63 (x is dead afterward)
	g.a.Cvttf2si(r, x, f64src, true)
	t := g.allocReg()
	g.a.MovImm64(t, 0x8000000000000000)
	g.a.Add64(r, t) // + 2^63
	g.freeReg(t)
	biasEnd := g.a.JmpPlaceholder()
	g.a.PatchRel32(simple, g.a.Len())
	g.a.Cvttf2si(r, x, f64src, true)
	g.a.PatchRel32(biasEnd, g.a.Len())
	g.freeFReg(p63)

	g.a.PatchRel32(endMax, g.a.Len())
	g.a.PatchRel32(end0, g.a.Len())
}
