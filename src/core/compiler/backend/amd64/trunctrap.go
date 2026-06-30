package amd64

import "math"

// Trapping float->int truncation (0xA8–0xB1: i32/i64.trunc_f32/f64_s/u).
// Unlike the saturating trunc_sat ops, a NaN or out-of-range input traps
// (TrapTruncOverflow). We trap first using the exact exclusive float bounds, so
// the conversion below always runs on an in-range value; cvtt truncates toward
// zero (unsigned needs a 64-bit / biased conversion, as in truncSat's in-range
// paths). loadFConst / floatBits are shared with truncsat.go.

// truncLimitBits returns the exclusive float bound bit patterns (in the source
// width) outside which a trunc to the given integer type must trap: x is valid
// iff min < x < max. Values mirror WARP's FloatTruncLimitsExcl.hpp.
func truncLimitBits(signed, f64src, dstWide bool) (minBits, maxBits uint64) {
	switch {
	case !f64src && signed && !dstWide: // i32 <- f32
		return 0xCF000001, 0x4F000000
	case !f64src && signed && dstWide: // i64 <- f32
		return 0xDF000001, 0x5F000000
	case !f64src && !signed && !dstWide: // u32 <- f32
		return 0xBF800000, 0x4F800000
	case !f64src && !signed && dstWide: // u64 <- f32
		return 0xBF800000, 0x5F800000
	case f64src && signed && !dstWide: // i32 <- f64
		return 0xC1E0000000200000, 0x41E0000000000000
	case f64src && signed && dstWide: // i64 <- f64
		return 0xC3E0000000000001, 0x43E0000000000000
	case f64src && !signed && !dstWide: // u32 <- f64
		return 0xBFF0000000000000, 0x41F0000000000000
	default: // u64 <- f64
		return 0xBFF0000000000000, 0x43F0000000000000
	}
}

func (g *cg) f2iTrunc(f64src, dstWide, signed bool) {
	x := g.materializeF(g.pop())
	r := g.allocReg()

	// Trap on NaN, x <= min, or x >= max; all three jump to a shared trap site,
	// jumped over by the in-range path.
	minBits, maxBits := truncLimitBits(signed, f64src, dstWide)
	g.a.Ucomis(x, x, f64src)
	jNaN := g.a.JccPlaceholder(CondP)
	lo := g.loadFConst(minBits, f64src)
	g.a.Ucomis(x, lo, f64src)
	g.freeFReg(lo)
	jLo := g.a.JccPlaceholder(CondBE)
	hi := g.loadFConst(maxBits, f64src)
	g.a.Ucomis(x, hi, f64src)
	g.freeFReg(hi)
	jHi := g.a.JccPlaceholder(CondAE)
	skip := g.a.JmpPlaceholder()
	trapAt := g.a.Len()
	g.emitTrap(trapTruncOverflow)
	g.a.PatchRel32(skip, g.a.Len())
	g.a.PatchRel32(jNaN, trapAt)
	g.a.PatchRel32(jLo, trapAt)
	g.a.PatchRel32(jHi, trapAt)

	switch {
	case signed:
		g.a.Cvttf2si(r, x, f64src, dstWide)
	case !dstWide: // u32: a 64-bit signed cvtt is exact on [0, 2^32)
		g.a.Cvttf2si(r, x, f64src, true)
	default: // u64
		g.truncU64InRange(x, r, f64src)
	}
	g.freeFReg(x)
	g.pushReg(r)
}

// truncU64InRange converts x, already proven in [0, 2^64), to u64. A signed cvtt
// overflows for x >= 2^63, so bias: cvtt(x - 2^63) + 2^63.
func (g *cg) truncU64InRange(x, r Reg, f64src bool) {
	p63 := g.loadFConst(floatBits(math.Ldexp(1, 63), f64src), f64src)
	g.a.Ucomis(x, p63, f64src)
	simple := g.a.JccPlaceholder(CondB) // x < 2^63: direct
	g.a.FSub(x, p63, f64src)            // x is dead afterward
	g.a.Cvttf2si(r, x, f64src, true)
	t := g.allocReg()
	g.a.MovImm64(t, 0x8000000000000000)
	g.a.Add64(r, t)
	g.freeReg(t)
	done := g.a.JmpPlaceholder()
	g.a.PatchRel32(simple, g.a.Len())
	g.a.Cvttf2si(r, x, f64src, true)
	g.a.PatchRel32(done, g.a.Len())
	g.freeFReg(p63)
}
