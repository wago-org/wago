package amd64

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// floatBits returns the bit pattern of v in the given float width.
func floatBits(v float64, f64 bool) uint64 {
	if f64 {
		return math.Float64bits(v)
	}
	return uint64(math.Float32bits(float32(v)))
}

// Floating point (f32/f64), ported from WARP's SSE lowering with the NaN- and
// signed-zero-correct sequences src/core/encoder/amd64 uses. Floats are handled eagerly:
// operands are materialized into XMM registers by a parallel allocator (fregUser/
// fpinned) and the result is pushed as an XMM-resident value. XMM and GP register
// namespaces are independent, so the integer condense engine is untouched.

// --- XMM allocator ---

func (f *fn) occupyF(e *elem, r Reg) {
	f.removeRef(e)
	f.fregUser[r] = e
	if e.kind == ekDeferred && e.typ != mtNone {
		e.st.typ = e.typ
	}
	e.kind = ekValue
	e.st.kind, e.st.reg = stReg, r
	f.addRef(e)
}

func (f *fn) releaseF(r Reg) {
	if r != regNone {
		f.fregUser[r] = nil
	}
}

// allocFReg returns a free XMM register, spilling the deepest float-resident stack
// value if none is free.
func (f *fn) allocFReg(avoid regMask) Reg {
	block := avoid.union(f.fpinned).union(f.fpinnedLocalMask)
	for r := Reg(0); r < 16; r++ {
		if f.fregUser[r] == nil && !block.has(r) {
			return r
		}
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stReg && e.st.typ.isXMM() && !block.has(e.st.reg) {
			r := e.st.reg
			f.spillF(e)
			return r
		}
	}
	panic("amd64: no XMM register available to spill")
}

// spillF evicts an XMM-resident float/vector value to a fresh frame slot.
func (f *fn) spillF(e *elem) {
	r := e.st.reg
	if e.st.typ == mtV128 {
		slot := f.allocSpillSlots(2)
		f.a.VMovdquStoreDisp(RSP, f.spillOff(slot), r)
		f.fregUser[r] = nil
		f.replaceStorage(e, storage{kind: stSlot, typ: e.st.typ, slot: slot})
		return
	}
	slot := f.allocSpillSlot()
	f.a.FStoreDisp(RSP, f.spillOff(slot), r, true)
	f.fregUser[r] = nil
	f.replaceStorage(e, storage{kind: stSlot, typ: e.st.typ, slot: slot})
}

// materializeF ensures float value e lives in an XMM register and returns it.
func (f *fn) materializeF(e *elem) Reg {
	switch e.st.kind {
	case stReg:
		return e.st.reg
	case stConst:
		x := f.allocFReg(0)
		f.loadFConst(x, e.st)
		f.occupyF(e, x)
		return x
	case stSlot:
		x := f.allocFReg(0)
		f.a.FLoadDisp(x, RSP, f.spillOff(e.st.slot), true) // 8B; f32 uses the low 4
		f.occupyF(e, x)
		return x
	case stLocalRef:
		x := f.allocFReg(0)
		f.a.FLoadDisp(x, RSP, f.localOff(e.st.idx), e.st.typ == mtF64)
		f.occupyF(e, x)
		return x
	case stLocalReg:
		// Borrowed pinned float local: copy into an owned XMM so the caller may
		// clobber it without corrupting the local.
		x := f.allocFReg(0)
		f.a.FMov(x, e.st.reg, e.st.typ == mtF64)
		f.occupyF(e, x)
		return x
	case stMemRef:
		x := f.allocFReg(0)
		f.loadFMemRef(x, e.st)
		f.releaseMemRef(e.st)
		f.occupyF(e, x)
		return x
	}
	panic("amd64: cannot materialize float storage")
}

// operandRegF returns a register holding e's value for READ-ONLY use as an SSE
// source operand (never written, so it need not be a private copy). A pinned float
// local is used directly and must not be released (owned=false); everything else is
// materialized into an owned scratch register the caller releases (owned=true).
// This avoids the movsd-to-scratch that materializeF emits for a pinned local when
// the value is only being read — the dominant per-op float overhead.
func (f *fn) operandRegF(e *elem) (reg Reg, owned bool) {
	if e.kind == ekValue && e.st.kind == stLocalReg {
		return e.st.reg, false
	}
	return f.materializeF(e), true
}

// pushFReg pushes an XMM-resident float value of the given type.
func (f *fn) pushFReg(r Reg, typ machineType) *elem {
	e := f.pushValue(storage{kind: stReg, typ: typ, reg: r})
	f.fregUser[r] = e
	return e
}

// loadFConst materializes a float constant's bits into XMM r (via a GP scratch).
func (f *fn) loadFConst(r Reg, st storage) {
	t := f.allocReg(0)
	if st.typ == mtF64 {
		f.a.MovImm64(t, uint64(st.cval))
		f.a.MovGprToXmm(r, t, true)
	} else {
		f.a.MovImm32(t, int32(uint32(st.cval)))
		f.a.MovGprToXmm(r, t, false)
	}
	f.release(t)
}

// loadFMask materializes a 32/64-bit bit mask into XMM dst (via a GP scratch).
func (f *fn) loadFMask(dst Reg, mask64 uint64, mask32 uint32, f64 bool) {
	t := f.allocReg(0)
	if f64 {
		f.a.MovImm64(t, mask64)
		f.a.MovGprToXmm(dst, t, true)
	} else {
		f.a.MovImm32(t, int32(mask32))
		f.a.MovGprToXmm(dst, t, false)
	}
	f.release(t)
}

// IEEE-754 sign/magnitude masks for neg, abs, copysign.
const (
	fSignMask32 uint32 = 0x80000000
	fMagMask32  uint32 = 0x7FFFFFFF
	fSignMask64 uint64 = 0x8000000000000000
	fMagMask64  uint64 = 0x7FFFFFFFFFFFFFFF
)

// Rounding-mode immediates for ROUNDSS/SD (bit 3 suppresses the inexact
// exception, matching wasm's non-trapping rounding).
const (
	roundNearest byte = 0x08
	roundFloor   byte = 0x09
	roundCeil    byte = 0x0A
	roundTrunc   byte = 0x0B
)

// --- float op handlers ---

func (f *fn) fconst(bits uint64, typ machineType) {
	f.pushValue(storage{kind: stConst, typ: typ, cval: int64(bits)})
}

// fbin lowers add/sub/mul/div via the 3-operand VEX form dst = s1 <op> s2. Both
// operands are read directly (a pinned local is borrowed, never copied), and the
// result lands in a reused owned-operand register or a fresh one — so no operand is
// pre-copied to scratch the way legacy 2-operand SSE requires.
func (f *fn) fbin(vop func(dst, s1, s2 Reg, f64 bool), memOp byte, f64 bool) {
	b := f.popValue()
	a := f.popValue()
	if b.kind == ekValue && b.st.kind == stMemRef && b.st.typ.isFloat() && b.st.memSize() == fsize(f64) {
		f.fbinMemRight(a, b, memOp, f64)
		return
	}
	s1, o1 := f.operandRegF(a)
	f.fpinned = f.fpinned.add(s1)
	s2, o2 := f.operandRegF(b)
	// Destination: reuse an owned operand's register in place (it is being
	// consumed), else a fresh register so a borrowed pinned local isn't clobbered.
	var dst Reg
	switch {
	case o1:
		dst = s1
	case o2:
		dst = s2
	default:
		// Both operands are borrowed pinned locals (blocked from allocation via the
		// pinned-local mask); s1 is also fpinned here, so a fresh dst avoids both.
		dst = f.allocFReg(0)
	}
	f.fpinned = f.fpinned.remove(s1)
	vop(dst, s1, s2, f64)
	if o1 && dst != s1 {
		f.releaseF(s1)
	}
	if o2 && dst != s2 {
		f.releaseF(s2)
	}
	f.pushFReg(dst, mtOf2(f64))
}

func (f *fn) fbinMemRight(a, b *elem, memOp byte, f64 bool) {
	src, owned := f.operandRegF(a)
	dst := src
	if !owned {
		dst = f.allocFReg(maskOf(src))
		f.a.FMov(dst, src, f64)
	}
	f.a.SseIdx(scalarFloatPrefix(f64), memOp, dst, RBX, b.st.reg, b.st.memDisp())
	f.releaseMemRef(b.st)
	f.pushFReg(dst, mtOf2(f64))
}

// fminmax implements wasm min/max, which x86 minss/maxss get wrong on signed
// zeros and NaN. Branch on the ordered compare; equal uses bitwise zero fixups,
// distinct ordered operands use packed min/max like wazero, and unordered
// propagates a quiet NaN through scalar add.
func (f *fn) fminmax(f64, isMax bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeF(a)
	f.fpinned = f.fpinned.add(xa)
	xb, xbOwned := f.operandRegF(b) // read-only: compared and combined into xa
	f.fpinned = f.fpinned.remove(xa)
	f.a.Ucomis(xa, xb, f64)
	jnan := f.a.JccPlaceholder(condP)
	jdist := f.a.JccPlaceholder(condNE)

	var prefix, bitOp byte
	if f64 {
		prefix = 0x66
	}
	if isMax {
		bitOp = 0x54 // andps/pd: max(-0,+0) = +0
	} else {
		bitOp = 0x56 // orps/pd: min(+0,-0) = -0
	}
	f.a.SseRR(prefix, bitOp, xa, xb, false)
	jdone := f.a.JmpPlaceholder()

	f.a.PatchRel32(jdist, f.a.Len())
	packedPrefix := byte(0)
	if f64 {
		packedPrefix = 0x66
	}
	if isMax {
		f.a.SseRR(packedPrefix, 0x5F, xa, xb, false) // maxps/pd, matching wazero
	} else {
		f.a.SseRR(packedPrefix, 0x5D, xa, xb, false) // minps/pd, matching wazero
	}
	jdone2 := f.a.JmpPlaceholder()

	f.a.PatchRel32(jnan, f.a.Len())
	f.a.FAdd(xa, xb, f64) // NaN + x -> quiet NaN, matching wazero

	f.a.PatchRel32(jdone, f.a.Len())
	f.a.PatchRel32(jdone2, f.a.Len())
	if xbOwned {
		f.releaseF(xb)
	}
	f.pushFReg(xa, mtOf2(f64))
}

func (f *fn) fsqrt(f64 bool) {
	src, owned := f.operandRegF(f.popValue())
	dst := src
	if !owned { // borrowed pinned local: write a fresh dest, leave the local intact
		dst = f.allocFReg(maskOf(src))
	}
	f.a.FSqrt(dst, src, f64)
	f.pushFReg(dst, mtOf2(f64))
}

// fsign applies a sign/magnitude bit op (neg = xorps sign; abs = andps magnitude)
// via the 3-operand VEX form so a borrowed pinned-local operand is read directly
// (dst = src <op> mask) instead of copied into the destination first.
func (f *fn) fsign(op byte, mask64 uint64, mask32 uint32, f64 bool) {
	src, owned := f.operandRegF(f.popValue())
	f.fpinned = f.fpinned.add(src)
	m := f.allocFReg(0)
	f.loadFMask(m, mask64, mask32, f64)
	f.fpinned = f.fpinned.remove(src)
	dst := src
	if !owned { // borrowed pinned local: write a fresh dest, leave the local intact
		dst = f.allocFReg(maskOf(src, m))
	}
	var pp byte // VEX pp: 66 (pd) for f64, none (ps) for f32
	if f64 {
		pp = 0b01
	}
	f.a.VSseRRR(pp, op, dst, src, m)
	f.releaseF(m)
	f.pushFReg(dst, mtOf2(f64))
}

func (f *fn) fneg(f64 bool) { f.fsign(0x57, fSignMask64, fSignMask32, f64) }
func (f *fn) fabs(f64 bool) { f.fsign(0x54, fMagMask64, fMagMask32, f64) }

func (f *fn) fround(f64 bool, mode byte) {
	src, owned := f.operandRegF(f.popValue())
	dst := src
	if !owned { // borrowed pinned local: round into a fresh dest, leave the local intact
		dst = f.allocFReg(maskOf(src))
	}
	f.a.Round(dst, src, f64, mode)
	f.pushFReg(dst, mtOf2(f64))
}

// fcopysign: (a & ~sign) | (b & sign).
func (f *fn) fcopysign(f64 bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeF(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeF(b)
	f.fpinned = f.fpinned.add(xb)
	var prefix byte
	if f64 {
		prefix = 0x66
	}
	m := f.allocFReg(0)
	f.loadFMask(m, fMagMask64, fMagMask32, f64)
	f.a.SseRR(prefix, 0x54, xa, m, false) // xa = |a|
	f.loadFMask(m, fSignMask64, fSignMask32, f64)
	f.a.SseRR(prefix, 0x54, xb, m, false) // xb = sign(b)
	f.releaseF(m)
	f.a.SseRR(prefix, 0x56, xa, xb, false) // xa |= xb
	f.fpinned = f.fpinned.remove(xa)
	f.fpinned = f.fpinned.remove(xb)
	f.releaseF(xb)
	f.pushFReg(xa, mtOf2(f64))
}

// fcmp lowers a NaN-correct float comparison to a 0/1 i32 result.
func (f *fn) fcmp(kind wOp, f64 bool) {
	b := f.popValue()
	a := f.popValue()
	xa, xaOwned := f.operandRegF(a) // read-only: only compared
	f.fpinned = f.fpinned.add(xa)
	xb, xbOwned := f.operandRegF(b) // read-only: only compared
	f.fpinned = f.fpinned.remove(xa)
	dst := f.allocReg(0)
	switch kind {
	case opEq:
		f.a.Ucomis(xa, xb, f64)
		t := f.allocReg(maskOf(dst))
		f.a.SetccReg(condE, dst)
		f.a.SetccReg(condNP, t)
		f.a.AluRR(aluTable[opAnd].rr, dst, t, false)
		f.release(t)
	case opNe:
		f.a.Ucomis(xa, xb, f64)
		t := f.allocReg(maskOf(dst))
		f.a.SetccReg(condNE, dst)
		f.a.SetccReg(condP, t)
		f.a.AluRR(aluTable[opOr].rr, dst, t, false)
		f.release(t)
	case opGtS: // fc gt
		f.a.Ucomis(xa, xb, f64)
		f.a.SetccReg(condA, dst)
	case opGeS: // fc ge
		f.a.Ucomis(xa, xb, f64)
		f.a.SetccReg(condAE, dst)
	case opLtS: // a<b == b>a (NaN-safe CF form)
		f.a.Ucomis(xb, xa, f64)
		f.a.SetccReg(condA, dst)
	case opLeS: // a<=b == b>=a
		f.a.Ucomis(xb, xa, f64)
		f.a.SetccReg(condAE, dst)
	}
	if xaOwned {
		f.releaseF(xa)
	}
	if xbOwned {
		f.releaseF(xb)
	}
	f.pushReg(dst, mtI32)
}

// i2f converts a signed integer to float. srcWide selects an i64 source.
func (f *fn) i2f(f64, srcWide bool) {
	gpr := f.materialize(f.popValue())
	xmm := f.allocFReg(0)
	f.a.Cvtsi2f(xmm, gpr, f64, srcWide)
	f.release(gpr)
	f.pushFReg(xmm, mtOf2(f64))
}

// i2fU converts an unsigned integer to float. For u32, zero-extend to i64 and do
// a signed convert (exact). For u64 with the top bit set, halve round-to-odd.
func (f *fn) i2fU(f64, srcWide bool) {
	if !srcWide { // u32: zero-extend then signed i64 convert
		gpr := f.materialize(f.popValue())
		f.a.MovRegReg32(gpr, gpr) // clear upper 32
		xmm := f.allocFReg(0)
		f.a.Cvtsi2f(xmm, gpr, f64, true)
		f.release(gpr)
		f.pushFReg(xmm, mtOf2(f64))
		return
	}
	gpr := f.materialize(f.popValue())
	f.pinned = f.pinned.add(gpr)
	xmm := f.allocFReg(0)
	f.a.TestSelf(gpr, true)
	big := f.a.JccPlaceholder(condS)
	f.a.Cvtsi2f(xmm, gpr, f64, true)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(big, f.a.Len())
	half := f.allocReg(maskOf(gpr))
	f.a.MovReg64(half, gpr)
	f.a.ShiftImm(5, half, 1, true) // shr half,1
	f.a.AluRI(4, gpr, 1, true)     // and gpr,1
	f.a.AluRR(0x09, half, gpr, true)
	f.a.Cvtsi2f(xmm, half, f64, true)
	f.a.FAdd(xmm, xmm, f64)
	f.release(half)
	f.a.PatchRel32(done, f.a.Len())
	f.pinned = f.pinned.remove(gpr)
	f.release(gpr)
	f.pushFReg(xmm, mtOf2(f64))
}

// truncLimitBits returns the exclusive source-width float bounds outside which a
// trunc to the given integer type must trap (x valid iff min < x < max). Mirrors
// src/core/encoder/amd64 / WARP FloatTruncLimitsExcl.
func truncLimitBits(signed, f64src, dstWide bool) (minBits, maxBits uint64) {
	switch {
	case !f64src && signed && !dstWide:
		return 0xCF000001, 0x4F000000
	case !f64src && signed && dstWide:
		return 0xDF000001, 0x5F000000
	case !f64src && !signed && !dstWide:
		return 0xBF800000, 0x4F800000
	case !f64src && !signed && dstWide:
		return 0xBF800000, 0x5F800000
	case f64src && signed && !dstWide:
		return 0xC1E0000000200000, 0x41E0000000000000
	case f64src && signed && dstWide:
		return 0xC3E0000000000001, 0x43E0000000000000
	case f64src && !signed && !dstWide:
		return 0xBFF0000000000000, 0x41F0000000000000
	default:
		return 0xBFF0000000000000, 0x43F0000000000000
	}
}

// loadFConstBits materializes raw float bits into a fresh XMM register.
func (f *fn) loadFConstBits(bits uint64, f64 bool) Reg {
	x := f.allocFReg(0)
	f.loadFConst(x, storage{typ: mtOf2(f64), cval: int64(bits)})
	return x
}

// f2iTrunc converts float→int with truncation, trapping (TruncOverflow) on NaN or
// out-of-range. srcF64 selects the source width; dstWide the i64 destination.
func (f *fn) f2iTrunc(dstWide, srcF64, signed bool) {
	x := f.materializeF(f.popValue())
	f.fpinned = f.fpinned.add(x)

	minBits, maxBits := truncLimitBits(signed, srcF64, dstWide)
	f.a.Ucomis(x, x, srcF64)
	f.trapIf(condP, trapTruncOverflow) // NaN
	lo := f.loadFConstBits(minBits, srcF64)
	f.a.Ucomis(x, lo, srcF64)
	f.releaseF(lo)
	f.trapIf(condBE, trapTruncOverflow) // x <= lower-exclusive limit
	hi := f.loadFConstBits(maxBits, srcF64)
	f.a.Ucomis(x, hi, srcF64)
	f.releaseF(hi)
	f.trapIf(condAE, trapTruncOverflow) // x >= upper-exclusive limit

	r := f.allocReg(0)
	switch {
	case signed:
		f.a.Cvttf2si(r, x, srcF64, dstWide)
	case !dstWide: // u32: a 64-bit signed cvtt is exact on [0, 2^32)
		f.a.Cvttf2si(r, x, srcF64, true)
	default: // u64
		f.truncU64InRange(x, r, srcF64)
	}
	f.fpinned = f.fpinned.remove(x)
	f.releaseF(x)
	f.pushReg(r, mtOfInt(dstWide))
}

// truncU64InRange converts x, already proven in [0, 2^64), to u64: a signed cvtt
// overflows for x >= 2^63, so bias by cvtt(x - 2^63) + 2^63.
func (f *fn) truncU64InRange(x, r Reg, srcF64 bool) {
	p63 := f.loadFConstBits(floatBits2p63(srcF64), srcF64)
	f.a.Ucomis(x, p63, srcF64)
	simple := f.a.JccPlaceholder(condB)
	f.a.FSub(x, p63, srcF64)
	f.a.Cvttf2si(r, x, srcF64, true)
	t := f.allocReg(maskOf(r))
	f.a.MovImm64(t, 0x8000000000000000)
	f.a.Add64(r, t)
	f.release(t)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(simple, f.a.Len())
	f.a.Cvttf2si(r, x, srcF64, true)
	f.a.PatchRel32(done, f.a.Len())
	f.releaseF(p63)
}

// floatBits2p63 returns the bit pattern of 2^63 in the given float width.
func floatBits2p63(f64 bool) uint64 { return floatBits(math.Ldexp(1, 63), f64) }

// --- saturating float→int truncation (0xFC 0-7): NaN→0, out-of-range clamps ---

func (f *fn) truncSat(f64src, dstWide, signed bool) {
	x := f.materializeF(f.popValue())
	f.fpinned = f.fpinned.add(x)
	r := f.allocReg(0)
	f.pinned = f.pinned.add(r)
	switch {
	case signed:
		f.truncSatSigned(x, r, f64src, dstWide)
	case dstWide:
		f.truncSatU64(x, r, f64src)
	default:
		f.truncSatU32(x, r, f64src)
	}
	f.pinned = f.pinned.remove(r)
	f.fpinned = f.fpinned.remove(x)
	f.releaseF(x)
	f.pushReg(r, mtOfInt(dstWide))
}

func (f *fn) truncSatSigned(x, r Reg, f64src, dstWide bool) {
	n := 32
	if dstWide {
		n = 64
	}
	f.a.Cvttf2si(r, x, f64src, dstWide)
	f.a.Ucomis(x, x, f64src)
	notNaN := f.a.JccPlaceholder(condNP)
	f.a.XorSelf32(r) // NaN → 0
	toEnd := f.a.JmpPlaceholder()
	f.a.PatchRel32(notNaN, f.a.Len())
	hi := f.loadFConstBits(floatBits(math.Ldexp(1, n-1), f64src), f64src) // 2^(n-1)
	f.a.Ucomis(x, hi, f64src)
	f.releaseF(hi)
	below := f.a.JccPlaceholder(condB)
	if dstWide {
		f.a.MovImm64(r, 0x7FFFFFFFFFFFFFFF)
	} else {
		f.a.MovImm32(r, 0x7FFFFFFF)
	}
	f.a.PatchRel32(below, f.a.Len())
	f.a.PatchRel32(toEnd, f.a.Len())
}

func (f *fn) truncSatU32(x, r Reg, f64src bool) {
	f.a.Cvttf2si(r, x, f64src, true) // i64 trunc; low 32 is the in-range u32
	zero := f.loadFConstBits(floatBits(0, f64src), f64src)
	f.a.Ucomis(x, zero, f64src)
	f.releaseF(zero)
	pos := f.a.JccPlaceholder(condA)
	f.a.XorSelf32(r) // NaN/≤0 → 0
	toEnd := f.a.JmpPlaceholder()
	f.a.PatchRel32(pos, f.a.Len())
	hi := f.loadFConstBits(floatBits(math.Ldexp(1, 32), f64src), f64src)
	f.a.Ucomis(x, hi, f64src)
	f.releaseF(hi)
	below := f.a.JccPlaceholder(condB)
	f.a.MovImm32(r, -1) // ≥2^32 → 0xFFFFFFFF
	f.a.PatchRel32(below, f.a.Len())
	f.a.PatchRel32(toEnd, f.a.Len())
}

func (f *fn) truncSatU64(x, r Reg, f64src bool) {
	zero := f.loadFConstBits(floatBits(0, f64src), f64src)
	f.a.Ucomis(x, zero, f64src)
	f.releaseF(zero)
	pos := f.a.JccPlaceholder(condA)
	f.a.XorSelf32(r)
	end0 := f.a.JmpPlaceholder()
	f.a.PatchRel32(pos, f.a.Len())
	hi := f.loadFConstBits(floatBits(math.Ldexp(1, 64), f64src), f64src)
	f.a.Ucomis(x, hi, f64src)
	f.releaseF(hi)
	inRange := f.a.JccPlaceholder(condB)
	f.a.MovImm64(r, 0xFFFFFFFFFFFFFFFF) // ≥2^64 → all ones
	endMax := f.a.JmpPlaceholder()
	f.a.PatchRel32(inRange, f.a.Len())
	p63 := f.loadFConstBits(floatBits2p63(f64src), f64src)
	f.a.Ucomis(x, p63, f64src)
	simple := f.a.JccPlaceholder(condB)
	f.a.FSub(x, p63, f64src)
	f.a.Cvttf2si(r, x, f64src, true)
	t := f.allocReg(maskOf(r))
	f.a.MovImm64(t, 0x8000000000000000)
	f.a.Add64(r, t)
	f.release(t)
	biasEnd := f.a.JmpPlaceholder()
	f.a.PatchRel32(simple, f.a.Len())
	f.a.Cvttf2si(r, x, f64src, true)
	f.a.PatchRel32(biasEnd, f.a.Len())
	f.releaseF(p63)
	f.a.PatchRel32(endMax, f.a.Len())
	f.a.PatchRel32(end0, f.a.Len())
}

func (f *fn) fpromote() { // f32 → f64
	x := f.materializeF(f.popValue())
	f.a.Cvtss2sd(x, x)
	f.pushFReg(x, mtF64)
}
func (f *fn) fdemote() { // f64 → f32
	x := f.materializeF(f.popValue())
	f.a.Cvtsd2ss(x, x)
	f.pushFReg(x, mtF32)
}

func (f *fn) reinterpretIntToFloat(wide bool) {
	gpr := f.materialize(f.popValue())
	xmm := f.allocFReg(0)
	f.a.MovGprToXmm(xmm, gpr, wide)
	f.release(gpr)
	f.pushFReg(xmm, mtOf2(wide))
}
func (f *fn) reinterpretFloatToInt(wide bool) {
	xmm := f.materializeF(f.popValue())
	gpr := f.allocReg(0)
	f.a.MovXmmToGpr(gpr, xmm, wide)
	f.releaseF(xmm)
	f.pushReg(gpr, mtOfInt(wide))
}

// fload / fstore reuse the integer bounds-checked effective-address path.
func (f *fn) fload(r *wasm.Reader, f64 bool) error {
	if _, err := r.U32(); err != nil {
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	size := 4
	if f64 {
		size = 8
	}
	ea, eaOwned, borrow, disp := f.memAddr(off, size, true)
	e := f.pushValue(fmemRefStorage(ea, disp, f64, borrow))
	if eaOwned {
		f.regUser[ea] = e
	}
	return nil
}

func (f *fn) fstore(r *wasm.Reader, f64 bool) error {
	if _, err := r.U32(); err != nil {
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	size := 4
	if f64 {
		size = 8
	}
	f.materializePendingLoads() // deferred loads must read pre-store memory
	xmm := f.materializeF(f.popValue())
	f.fpinned = f.fpinned.add(xmm)
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	f.a.FStoreIdx(RBX, ea, xmm, disp, f64)
	f.fpinned = f.fpinned.remove(xmm)
	if eaOwned {
		f.release(ea)
	}
	f.releaseF(xmm)
	return nil
}

// helpers

func (f *fn) loadFMemRef(dst Reg, st storage) {
	f.a.FLoadIdx(dst, RBX, st.reg, st.memDisp(), st.typ == mtF64)
}

func fsize(f64 bool) int {
	if f64 {
		return 8
	}
	return 4
}

func scalarFloatPrefix(f64 bool) byte {
	if f64 {
		return 0xF2
	}
	return 0xF3
}

func mtOf2(f64 bool) machineType {
	if f64 {
		return mtF64
	}
	return mtF32
}
func mtOfInt(wide bool) machineType {
	if wide {
		return mtI64
	}
	return mtI32
}
