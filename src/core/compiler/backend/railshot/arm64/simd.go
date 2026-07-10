//go:build arm64

package arm64

import (
	"encoding/binary"
	"fmt"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// This file is the arm64 (NEON) twin of amd64/simd.go. The neutral operand-stack
// / register-allocation / pin machinery ports verbatim; only the leaf encoder
// calls change from SSE/AVX (VP*/VSse*/VF*) to their NEON equivalents on the a64
// encoder. A few SSE-idiom sequences intentionally keep a different arm64 shape:
// NEON has direct TBL/CNT/BSL/widen/narrow/conversion ops, and packed fixups are
// used where one NEON instruction alone does not preserve WebAssembly edge
// semantics.
//
// `a64` import is used indirectly through Reg/Cond aliases declared in cc.go; the
// blank reference below keeps the import live if no direct symbol is used here.
var _ = a64.X0

func (f *fn) materializeV128(e *elem) Reg {
	if e.isDeferred() {
		panic("arm64: deferred v128 op not supported")
	}
	switch e.st.kind {
	case stReg:
		return e.st.reg
	case stConst:
		if e.st.typ == mtV128 && e.st.cval == 0 {
			x := f.allocFReg(0)
			f.a.NeonEor16b(x, x, x)
			f.occupyF(e, x)
			return x
		}
	case stSlot:
		x := f.allocFReg(0)
		f.a.LdrQ(x, SP, f.spillOff(e.st.slot))
		f.occupyF(e, x)
		return x
	case stLocalRef:
		x := f.allocFReg(0)
		f.a.LdrQ(x, SP, f.localOff(e.st.idx))
		f.occupyF(e, x)
		return x
	case stLocalReg:
		// Pinned v128 local: the live value is in the V register (the slot may be
		// stale). Copy into an owned scratch so a destructive op on the result cannot
		// corrupt the local — mirrors the scalar-float materializeF stLocalReg copy.
		x := f.allocFReg(0)
		f.a.NeonMov16b(x, e.st.reg)
		f.occupyF(e, x)
		return x
	}
	panic("arm64: cannot materialize v128 storage")
}

// operandRegV128 returns a register holding e's value for READ-ONLY use as a NEON
// source operand (never written, so it need not be a private copy). A pinned v128
// local is used directly and must not be released (owned=false); everything else is
// materialized into an owned scratch the caller releases. This avoids the
// register-to-register copy materializeV128 emits for a pinned local when the value
// is only being read.
func (f *fn) operandRegV128(e *elem) (reg Reg, owned bool) {
	if e.kind == ekValue && e.st.kind == stLocalReg {
		return e.st.reg, false
	}
	return f.materializeV128(e), true
}

func (f *fn) pushVReg(r Reg) *elem {
	e := f.pushValue(storage{kind: stReg, typ: mtV128, reg: r})
	f.fregUser[r] = e
	return e
}

func (f *fn) stV128(base Reg, disp int32, rt Reg) {
	f.a.StrQ(base, disp, rt)
}

func (f *fn) v128ConstReg(lo, hi uint64) Reg {
	x := f.allocFReg(0)
	if lo == 0 && hi == 0 {
		f.a.NeonEor16b(x, x, x)
		return x
	}
	t := f.allocReg(0)
	f.a.MovImm64(t, lo)
	f.a.FmovFromGpr(x, t, true) // FMOV Dn,Xt zeroes the high 64 bits.
	if hi != 0 {
		f.a.MovImm64(t, hi)
		f.a.NeonInsD(x, t, 1)
	}
	f.release(t)
	return x
}

func (f *fn) v128Const(lo, hi uint64) {
	f.pushVReg(f.v128ConstReg(lo, hi))
}

func (f *fn) v128UnaryNot() {
	a := f.popValue()
	x := f.materializeV128(a)
	f.a.NeonNot16b(x, x)
	f.pushVReg(x)
}

func (f *fn) v128IntegerNeg(op func(dst, src Reg)) {
	a := f.popValue()
	x := f.materializeV128(a)
	op(x, x)
	f.pushVReg(x)
}

func (f *fn) v128IntegerAbs(op func(dst, src Reg)) {
	a := f.popValue()
	x := f.materializeV128(a)
	op(x, x)
	f.pushVReg(x)
}

func (f *fn) v128FloatRound(f64 bool, mode byte) {
	a := f.popValue()
	x := f.materializeV128(a)
	f.a.NeonFrint(x, x, f64, mode)
	f.pushVReg(x)
}

func (f *fn) i8x16Popcnt() {
	v := f.popValue()
	x := f.materializeV128(v)
	f.a.NeonCntB(x, x)
	f.pushVReg(x)
}

func v128MaskBits(b [16]byte) (uint64, uint64) {
	return binary.LittleEndian.Uint64(b[0:8]), binary.LittleEndian.Uint64(b[8:16])
}

func (f *fn) i8x16Swizzle() {
	idxElem := f.popValue()
	srcElem := f.popValue()
	idx := f.materializeV128(idxElem)
	f.fpinned = f.fpinned.add(idx)
	src := f.materializeV128(srcElem)

	f.a.NeonTbl(src, src, idx)
	f.fpinned = f.fpinned.remove(idx)
	f.releaseF(idx)
	f.pushVReg(src)
}

func (f *fn) i8x16Shuffle(lanes [16]byte) {
	var aMask, bMask [16]byte
	for i := range aMask {
		aMask[i], bMask[i] = 0x80, 0x80
	}
	for i, lane := range lanes {
		if lane < 16 {
			aMask[i] = lane
		} else {
			bMask[i] = lane - 16
		}
	}

	bElem := f.popValue()
	aElem := f.popValue()
	xa := f.materializeV128(aElem)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(bElem)
	f.fpinned = f.fpinned.add(xb)

	lo, hi := v128MaskBits(aMask)
	ma := f.v128ConstReg(lo, hi)
	f.fpinned = f.fpinned.add(ma)
	lo, hi = v128MaskBits(bMask)
	mb := f.v128ConstReg(lo, hi)

	f.a.NeonTbl(xa, xa, ma)
	f.fpinned = f.fpinned.remove(ma)
	f.releaseF(ma)
	f.a.NeonTbl(xb, xb, mb)
	f.releaseF(mb)
	f.fpinned = f.fpinned.remove(xa).remove(xb)
	f.a.NeonOrr16b(xa, xa, xb)
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) v128Bin(op func(dst, s1, s2 Reg)) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a) // owned writable copy: op writes s1
	f.fpinned = f.fpinned.add(xa)
	xb, bOwned := f.operandRegV128(b) // read-only source: a pinned local is used in place
	f.fpinned = f.fpinned.remove(xa)
	op(xa, xa, xb)
	if bOwned {
		f.releaseF(xb)
	}
	f.pushVReg(xa)
}

func (f *fn) v128NarrowI16x8ToI8x16(signed bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	if signed {
		f.a.NeonSqxtnBfromH(xa, xa)
		f.a.NeonSqxtn2BfromH(xa, xb)
	} else {
		f.a.NeonSqxtunBfromH(xa, xa)
		f.a.NeonSqxtun2BfromH(xa, xb)
	}

	f.fpinned = f.fpinned.remove(xa)
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) v128NarrowI32x4ToI16x8(signed bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	if signed {
		f.a.NeonSqxtnHfromS(xa, xa)
		f.a.NeonSqxtn2HfromS(xa, xb)
	} else {
		f.a.NeonSqxtunHfromS(xa, xa)
		f.a.NeonSqxtun2HfromS(xa, xb)
	}

	f.fpinned = f.fpinned.remove(xa)
	f.releaseF(xb)
	f.pushVReg(xa)
}

// v128FloatMinMax uses AArch64's IEEE-propagating FMIN/FMAX directly. Unlike
// x86 MINPS/MAXPS, these instructions return NaN when either operand is NaN and
// select -0 for min / +0 for max, exactly the deterministic parts of Wasm's
// semantics. Wasm permits any quiet arithmetic NaN payload, so canonicalizing
// every NaN lane in software only adds latency.
func (f *fn) v128FloatMinMax(f64, isMax bool) {
	if isMax {
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFmax(dst, s1, s2, f64) })
	} else {
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFmin(dst, s1, s2, f64) })
	}
}

func (f *fn) v128FloatPMinMax(f64, isMax bool) {
	bElem := f.popValue()
	aElem := f.popValue()
	xa := f.materializeV128(aElem)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(bElem)
	f.fpinned = f.fpinned.add(xb)

	// Pseudo-min/max chooses b only when it is strictly smaller/larger than a.
	// Ordered FCMP is false for either NaN, so a also wins equal, signed-zero,
	// first-NaN, and second-NaN lanes exactly as required.
	mask := f.allocFReg(maskOf(xa, xb))
	if isMax {
		f.a.NeonFcmp(mask, xb, xa, f64, vfcmpGtOQ)
	} else {
		f.a.NeonFcmp(mask, xb, xa, f64, vfcmpLtOQ)
	}
	f.a.NeonBsl16b(mask, xb, xa)

	f.fpinned = f.fpinned.remove(xa).remove(xb)
	f.releaseF(xa)
	f.releaseF(xb)
	f.pushVReg(mask)
}

func (f *fn) v128Bitselect() {
	maskElem := f.popValue()
	bElem := f.popValue()
	aElem := f.popValue()
	mask := f.materializeV128(maskElem)
	f.fpinned = f.fpinned.add(mask)
	xb := f.materializeV128(bElem)
	f.fpinned = f.fpinned.add(xb)
	xa := f.materializeV128(aElem)
	f.a.NeonBsl16b(mask, xa, xb)
	f.fpinned = f.fpinned.remove(mask).remove(xb)
	f.releaseF(xb)
	f.releaseF(xa)
	f.pushVReg(mask)
}

func (f *fn) v128RelaxedMadd(f64, neg bool) {
	cElem := f.popValue()
	bElem := f.popValue()
	aElem := f.popValue()
	xa := f.materializeV128(aElem)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(bElem)
	f.fpinned = f.fpinned.add(xb)
	xc := f.materializeV128(cElem)

	f.a.NeonFmul(xa, xa, xb, f64)
	f.fpinned = f.fpinned.remove(xb)
	f.releaseF(xb)
	if neg {
		f.a.NeonFsub(xc, xc, xa, f64) // relaxed_nmadd: c - (a * b), without FMA.
		f.fpinned = f.fpinned.remove(xa)
		f.releaseF(xa)
		f.pushVReg(xc)
		return
	}
	f.a.NeonFadd(xa, xa, xc, f64)
	f.releaseF(xc)
	f.fpinned = f.fpinned.remove(xa)
	f.pushVReg(xa)
}

func (f *fn) v128I32x4TruncSat(f64src, signed bool) {
	srcElem := f.popValue()
	src := f.materializeV128(srcElem)
	if !f64src {
		if signed {
			f.a.NeonFcvtzsSfromS(src, src)
		} else {
			f.a.NeonFcvtzuSfromS(src, src)
		}
		f.pushVReg(src)
		return
	}
	// FCVTZ{S,U} converts both f64 lanes to 64-bit integers with WebAssembly's
	// required NaN/overflow saturation. The saturating narrow then produces the
	// two i32 lanes and clears the high half of the destination vector.
	if signed {
		f.a.NeonFcvtzsDfromD(src, src)
		f.a.NeonSqxtnSfromD(src, src)
	} else {
		f.a.NeonFcvtzuDfromD(src, src)
		f.a.NeonUqxtnSfromD(src, src)
	}
	f.pushVReg(src)
}

func (f *fn) v128DemoteF64x2Zero() {
	srcElem := f.popValue()
	src := f.materializeV128(srcElem)
	f.fpinned = f.fpinned.add(src)

	out := f.allocFReg(maskOf(src))
	f.a.NeonEor16b(out, out, out)
	f.a.NeonFcvtnSfromD(out, src)
	f.fpinned = f.fpinned.remove(src)
	f.releaseF(src)
	f.pushVReg(out)
}

func (f *fn) v128PromoteLowF32x4() {
	srcElem := f.popValue()
	src := f.materializeV128(srcElem)
	f.a.NeonFcvtlDfromS(src, src)
	f.pushVReg(src)
}

func (f *fn) v128I32x4ConvertToFloat(f64dst, signed bool) {
	srcElem := f.popValue()
	src := f.materializeV128(srcElem)
	if !f64dst {
		if signed {
			f.a.NeonScvtfSfromS(src, src)
		} else {
			f.a.NeonUcvtfSfromS(src, src)
		}
		f.pushVReg(src)
		return
	}
	if signed {
		f.a.NeonSxtlDfromS(src, src)
		f.a.NeonScvtfDfromD(src, src)
	} else {
		f.a.NeonUxtlDfromS(src, src)
		f.a.NeonUcvtfDfromD(src, src)
	}
	f.pushVReg(src)
}

func (f *fn) v128Shift(op func(dst, s1, s2 Reg), countMask int32, laneSize int, right bool) {
	countElem := f.popValue()
	count := f.materialize(countElem)
	f.andImm(count, int64(countMask), false) // Wasm shifts use count modulo lane width.
	if right {
		f.a.Sub64(count, ZR, count) // NEON USHL/SSHL use negative counts for right shifts.
	}

	value := f.popValue()
	x := f.materializeV128(value)
	f.fpinned = f.fpinned.add(x)
	countX := f.v128SplatScalar(count, laneSize)
	f.release(count)

	op(x, x, countX)
	f.releaseF(countX)
	f.fpinned = f.fpinned.remove(x)
	f.pushVReg(x)
}

func (f *fn) i8x16Shift(op func(dst, s1, s2 Reg), right bool) { f.v128Shift(op, 7, 1, right) }

func (f *fn) i16x8Shift(op func(dst, s1, s2 Reg), right bool) { f.v128Shift(op, 15, 2, right) }

func (f *fn) i32x4Shift(op func(dst, s1, s2 Reg), right bool) { f.v128Shift(op, 31, 4, right) }

func (f *fn) i64x2Shift(op func(dst, s1, s2 Reg), right bool) { f.v128Shift(op, 63, 8, right) }

// i64x2ShrS: arm64 has no packed arithmetic 64-bit right shift on the base NEON
// profile, so extract each lane to a GPR and use the orthogonal ASRV. The amd64
// "force count into RCX / spill RCX / pin RCX" dance disappears — ASRV takes any
// register as the shift amount (see CONTRACT §4c).
func (f *fn) i64x2ShrS() {
	countElem := f.popValue()
	count := f.materialize(countElem)
	f.a.AndImm32(count, count, 63) // Wasm shifts use count modulo lane width.
	f.pinned = f.pinned.add(count)

	value := f.popValue()
	x := f.materializeV128(value)
	lo := f.allocReg(maskOf(count))
	f.pinned = f.pinned.add(lo)
	hi := f.allocReg(maskOf(count, lo))

	f.a.FmovToGpr(lo, x, true)
	f.a.NeonUmovD(hi, x, 1)
	f.a.Asrv64(lo, lo, count) // asr lo, lo, count
	f.a.Asrv64(hi, hi, count) // asr hi, hi, count
	f.a.FmovFromGpr(x, lo, true)
	f.a.NeonInsD(x, hi, 1)

	f.release(hi)
	f.pinned = f.pinned.remove(lo)
	f.release(lo)
	f.pinned = f.pinned.remove(count)
	f.release(count)
	f.pushVReg(x)
}

func (f *fn) i64x2Abs() {
	value := f.popValue()
	x := f.materializeV128(value)
	f.a.NeonAbsD(x, x)
	f.pushVReg(x)
}

// TODO(arm64): NEON has no single-instruction 64-bit lane multiply; the standard
// lowering is a widening-and-recombine (or the extract-to-GPR path below). Keep
// the GPR path for correctness parity.
func (f *fn) i64x2Mul() {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	aLo := f.allocReg(0)
	f.pinned = f.pinned.add(aLo)
	aHi := f.allocReg(maskOf(aLo))
	f.pinned = f.pinned.add(aHi)
	bLo := f.allocReg(maskOf(aLo, aHi))
	f.pinned = f.pinned.add(bLo)
	bHi := f.allocReg(maskOf(aLo, aHi, bLo))

	f.a.FmovToGpr(aLo, xa, true)
	f.a.NeonUmovD(aHi, xa, 1)
	f.a.FmovToGpr(bLo, xb, true)
	f.a.NeonUmovD(bHi, xb, 1)
	f.a.Mul64(aLo, aLo, bLo)
	f.a.Mul64(aHi, aHi, bHi)
	f.a.FmovFromGpr(xa, aLo, true)
	f.a.NeonInsD(xa, aHi, 1)

	f.release(bHi)
	f.pinned = f.pinned.remove(bLo)
	f.release(bLo)
	f.pinned = f.pinned.remove(aHi)
	f.release(aHi)
	f.pinned = f.pinned.remove(aLo)
	f.release(aLo)
	f.fpinned = f.fpinned.remove(xb)
	f.releaseF(xb)
	f.fpinned = f.fpinned.remove(xa)
	f.pushVReg(xa)
}

func (f *fn) i16x8ExtendI8x16(signed, high bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	switch {
	case signed && high:
		f.a.NeonSxtl2HfromB(x, x)
	case signed:
		f.a.NeonSxtlHfromB(x, x)
	case high:
		f.a.NeonUxtl2HfromB(x, x)
	default:
		f.a.NeonUxtlHfromB(x, x)
	}
	f.pushVReg(x)
}

func (f *fn) i16x8ExtaddPairwiseI8x16(signed bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	if signed {
		f.a.NeonSaddlpHfromB(x, x)
	} else {
		f.a.NeonUaddlpHfromB(x, x)
	}
	f.pushVReg(x)
}

func (f *fn) i16x8ExtmulI8x16(signed, high bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	f.fpinned = f.fpinned.remove(xa).remove(xb)
	switch {
	case signed && high:
		f.a.NeonSmull2HfromB(xa, xa, xb)
	case signed:
		f.a.NeonSmullHfromB(xa, xa, xb)
	case high:
		f.a.NeonUmull2HfromB(xa, xa, xb)
	default:
		f.a.NeonUmullHfromB(xa, xa, xb)
	}
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) i32x4ExtendI16x8(signed, high bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	switch {
	case signed && high:
		f.a.NeonSxtl2SfromH(x, x)
	case signed:
		f.a.NeonSxtlSfromH(x, x)
	case high:
		f.a.NeonUxtl2SfromH(x, x)
	default:
		f.a.NeonUxtlSfromH(x, x)
	}
	f.pushVReg(x)
}

func (f *fn) i32x4ExtmulI16x8(signed, high bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	f.fpinned = f.fpinned.remove(xa).remove(xb)
	switch {
	case signed && high:
		f.a.NeonSmull2SfromH(xa, xa, xb)
	case signed:
		f.a.NeonSmullSfromH(xa, xa, xb)
	case high:
		f.a.NeonUmull2SfromH(xa, xa, xb)
	default:
		f.a.NeonUmullSfromH(xa, xa, xb)
	}
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) i32x4ExtaddPairwiseI16x8(signed bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	if signed {
		f.a.NeonSaddlpSfromH(x, x)
	} else {
		f.a.NeonUaddlpSfromH(x, x)
	}
	f.pushVReg(x)
}

func (f *fn) i64x2ExtendI32x4(signed, high bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	switch {
	case signed && high:
		f.a.NeonSxtl2DfromS(x, x)
	case signed:
		f.a.NeonSxtlDfromS(x, x)
	case high:
		f.a.NeonUxtl2DfromS(x, x)
	default:
		f.a.NeonUxtlDfromS(x, x)
	}
	f.pushVReg(x)
}

func (f *fn) i64x2ExtmulI32x4(signed, high bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	f.fpinned = f.fpinned.remove(xa).remove(xb)
	switch {
	case signed && high:
		f.a.NeonSmull2DfromS(xa, xa, xb)
	case signed:
		f.a.NeonSmullDfromS(xa, xa, xb)
	case high:
		f.a.NeonUmull2DfromS(xa, xa, xb)
	default:
		f.a.NeonUmullDfromS(xa, xa, xb)
	}
	f.releaseF(xb)
	f.pushVReg(xa)
}

// relaxedDotI8x16I7x16 returns eight signed, saturating pair sums. Widening
// multiplies preserve every i8 product, SADDLP forms exact i32 pair sums, and
// SQXTN performs the same i16 saturation as the former scalar clamp loop.
func (f *fn) relaxedDotI8x16I7x16() Reg {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)
	lo := f.allocFReg(maskOf(xa, xb))

	f.a.NeonSmullHfromB(lo, xa, xb)
	f.a.NeonSmull2HfromB(xa, xa, xb)
	f.a.NeonSaddlpSfromH(lo, lo)
	f.a.NeonSaddlpSfromH(xa, xa)
	f.a.NeonSqxtnHfromS(lo, lo)
	f.a.NeonSqxtn2HfromS(lo, xa)

	f.fpinned = f.fpinned.remove(xa).remove(xb)
	f.releaseF(xb)
	f.releaseF(xa)
	return lo
}

func (f *fn) i16x8RelaxedDotI8x16I7x16S() {
	f.pushVReg(f.relaxedDotI8x16I7x16())
}

func (f *fn) i32x4RelaxedDotI8x16I7x16AddS() {
	cElem := f.popValue()
	xc := f.materializeV128(cElem)
	f.fpinned = f.fpinned.add(xc)
	out := f.relaxedDotI8x16I7x16()
	f.a.NeonSaddlpSfromH(out, out)
	f.a.NeonAddS(out, out, xc)
	f.fpinned = f.fpinned.remove(xc)
	f.releaseF(xc)
	f.pushVReg(out)
}

func (f *fn) i32x4DotI16x8S() {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)
	out := f.allocFReg(maskOf(xa, xb))
	f.a.NeonSmullSfromH(out, xa, xb)
	f.a.NeonSmull2SfromH(xa, xa, xb)
	f.a.NeonAddpS(out, out, xa)

	f.fpinned = f.fpinned.remove(xa).remove(xb)
	f.releaseF(xb)
	f.releaseF(xa)
	f.pushVReg(out)
}

// i16x8Q15mulrSatS lowers directly to a single SQRDMULH. AArch64's saturating
// rounding doubling multiply-high computes signed_saturate((a*b + 0x4000) >> 15)
// with an infinite-precision intermediate, so the doubling of INT16_MIN*INT16_MIN
// saturates to 0x7fff exactly as Wasm requires (unlike x86 PMULHRSW, which wraps
// to 0x8000 — the very reason the relaxed variant exists). No software fixup for
// the overflow lane is needed; the earlier CMEQ/AND/ANDN/ORR dance was redundant.
func (f *fn) i16x8Q15mulrSatS() {
	f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonSqrdmulhH(dst, s1, s2) })
}

func (f *fn) v128BinNot(op func(dst, s1, s2 Reg)) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.remove(xa)
	op(xa, xa, xb)
	f.releaseF(xb)
	m := f.allocFReg(maskOf(xa))
	f.a.NeonCmeqB(m, m, m)
	f.a.NeonEor16b(xa, xa, m)
	f.releaseF(m)
	f.pushVReg(xa)
}

func (f *fn) v128SignedCmp(op func(dst, s1, s2 Reg), swap, invert bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.remove(xa)
	if swap {
		op(xa, xb, xa)
	} else {
		op(xa, xa, xb)
	}
	f.releaseF(xb)
	if invert {
		f.a.NeonNot16b(xa, xa)
	}
	f.pushVReg(xa)
}

func (f *fn) v128UnsignedCmp(op func(dst, s1, s2 Reg), swap bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.remove(xa)
	if swap {
		op(xa, xb, xa)
	} else {
		op(xa, xa, xb)
	}
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) i64x2SignedCmp(cc Cond) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.remove(xa)
	switch cc {
	case condL:
		f.a.NeonCmgtD(xa, xb, xa)
	case condG:
		f.a.NeonCmgtD(xa, xa, xb)
	case condLE:
		f.a.NeonCmgeD(xa, xb, xa)
	case condGE:
		f.a.NeonCmgeD(xa, xa, xb)
	default:
		panic("arm64: unsupported i64x2 signed compare")
	}
	f.fpinned = f.fpinned.remove(xb)
	f.releaseF(xb)
	f.pushVReg(xa)
}

// Float-compare predicates. On arm64 NeonFcmp maps ordered comparisons to the
// FCMEQ/FCMGT/FCMGE family (plus operand swap for lt/le). NaN lanes compare
// false for ordered predicates; ne is implemented as inverted eq so unordered
// lanes become true.
const (
	vfcmpEqOQ  = 0x00 // ordered, quiet: false for NaN lanes
	vfcmpNeqUQ = 0x04 // unordered or not-equal, quiet: true for NaN lanes
	vfcmpLtOQ  = 0x11 // ordered, quiet
	vfcmpLeOQ  = 0x12 // ordered, quiet
	vfcmpGeOQ  = 0x1d // ordered, quiet
	vfcmpGtOQ  = 0x1e // ordered, quiet
)

func (f *fn) v128FCmp(f64 bool, pred byte) {
	if pred == vfcmpNeqUQ {
		f.v128BinNot(func(dst, s1, s2 Reg) { f.a.NeonFcmp(dst, s1, s2, f64, vfcmpEqOQ) })
		return
	}
	f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFcmp(dst, s1, s2, f64, pred) })
}

// v128Movemask extracts each byte lane's sign bit into the matching bit of an
// i32 result. After a packed shift, each 64-bit half contains eight one-bit
// bytes. Multiplication by this bit-gather constant moves those bits into the
// high byte, avoiding sixteen UMOV/shift/OR lane sequences.
func (f *fn) v128MovemaskReg(x Reg) Reg {
	r := f.allocReg(0)
	hi := f.allocReg(maskOf(r))
	magic := f.allocReg(maskOf(r, hi))
	f.a.NeonUshrB(x, x, 7)
	f.a.FmovToGpr(r, x, true)
	f.a.NeonUmovD(hi, x, 1)
	f.a.MovImm64(magic, 0x0102040810204080)
	f.a.Mul64(r, r, magic)
	f.a.Mul64(hi, hi, magic)
	f.a.LsrImm(r, r, 56, false)
	f.a.LsrImm(hi, hi, 56, false)
	f.a.LslImm(hi, hi, 8, true)
	f.a.Orr32(r, r, hi)
	f.release(magic)
	f.release(hi)
	return r
}

func (f *fn) v128Movemask() Reg {
	v := f.popValue()
	x := f.materializeV128(v)
	r := f.v128MovemaskReg(x)
	f.releaseF(x)
	return r
}

func (f *fn) v128AnyTrue() {
	v := f.popValue()
	x := f.materializeV128(v)
	f.a.NeonUmaxvB(x, x)
	r := f.allocReg(0)
	f.a.NeonUmovB(r, x, 0)
	f.releaseF(x)
	f.a.CmpImm32(r, 0)
	f.a.Cset32(r, condNE)
	f.pushReg(r, mtI32)
}

func (f *fn) v128AllTrue(cmpEqZero func(dst, s1, s2 Reg)) {
	v := f.popValue()
	x := f.materializeV128(v)
	z := f.allocFReg(maskOf(x))
	f.a.NeonEor16b(z, z, z)
	cmpEqZero(x, x, z) // lanes are all-ones only where the original lane was zero.
	f.releaseF(z)
	f.a.NeonUmaxvB(x, x)
	r := f.allocReg(0)
	f.a.NeonUmovB(r, x, 0)
	f.releaseF(x)
	f.a.CmpImm32(r, 0)
	f.a.Cset32(r, condE)
	f.pushReg(r, mtI32)
}

// v128AllTrueMin lowers all_true for the 8/16/32-bit lane widths that UMINV
// supports: every lane is non-zero iff the unsigned horizontal minimum lane is
// non-zero. This is a single reduce + move + test, replacing the
// zero-compare + EOR + UMAXV sequence.
func (f *fn) v128AllTrueMin(uminv func(dst, src Reg)) {
	v := f.popValue()
	x := f.materializeV128(v)
	uminv(x, x) // low lane holds the min; the reduction zeroes the upper bits.
	r := f.allocReg(0)
	f.a.FmovToGpr(r, x, false)
	f.releaseF(x)
	f.a.CmpImm32(r, 0)
	f.a.Cset32(r, condNE)
	f.pushReg(r, mtI32)
}

func (f *fn) i8x16AllTrue() { f.v128AllTrueMin(f.a.NeonUminvB) }

func (f *fn) i16x8AllTrue() { f.v128AllTrueMin(f.a.NeonUminvH) }

func (f *fn) i32x4AllTrue() { f.v128AllTrueMin(f.a.NeonUminvS) }

// i64x2 has no UMINV.2d, so it keeps the compare-against-zero reduction.
func (f *fn) i64x2AllTrue() { f.v128AllTrue(f.a.NeonCmeqD) }

func (f *fn) i8x16Bitmask() {
	r := f.v128Movemask()
	f.pushReg(r, mtI32)
}

// v128MaskReg materializes a 128-bit constant into a fresh V register without
// clobbering the caller's live operand(s) named in avoid.
func (f *fn) v128MaskReg(lo, hi uint64, avoid regMask) Reg {
	m := f.allocFReg(avoid)
	t := f.allocReg(0)
	f.a.MovImm64(t, lo)
	f.a.FmovFromGpr(m, t, true) // FMOV Dn,Xt zeroes the high 64 bits.
	f.a.MovImm64(t, hi)
	f.a.NeonInsD(m, t, 1)
	f.release(t)
	return m
}

// bitmaskAddv lowers bitmask for the 16/32-bit lane widths. A signed shift by
// (laneBits-1) broadcasts each lane's sign bit to an all-ones/zero lane, ANDing
// with a per-lane power-of-two weight leaves a distinct bit per set lane, and a
// horizontal ADDV sums those disjoint bits into the packed mask.
func (f *fn) bitmaskAddv(lo, hi uint64, sshr func(dst, n Reg, shift uint8), shift uint8, addv func(dst, src Reg)) {
	v := f.popValue()
	x := f.materializeV128(v)
	mask := f.v128MaskReg(lo, hi, maskOf(x))
	sshr(x, x, shift)
	f.a.NeonAnd16b(x, x, mask)
	f.releaseF(mask)
	addv(x, x)
	r := f.allocReg(0)
	f.a.FmovToGpr(r, x, false)
	f.releaseF(x)
	f.pushReg(r, mtI32)
}

func (f *fn) i16x8Bitmask() {
	// Per-lane weights {1,2,4,8,16,32,64,128} as eight i16 lanes.
	f.bitmaskAddv(0x0008000400020001, 0x0080004000200010, f.a.NeonSshrH, 15, f.a.NeonAddvH)
}

func (f *fn) i32x4Bitmask() {
	// Per-lane weights {1,2,4,8} as four i32 lanes.
	f.bitmaskAddv(0x0000000200000001, 0x0000000800000004, f.a.NeonSshrS, 31, f.a.NeonAddvS)
}

func (f *fn) i64x2Bitmask() {
	// Only two lanes: shift each sign bit down to bit 0, then place lane 1 at bit 1.
	v := f.popValue()
	x := f.materializeV128(v)
	f.a.NeonUshrD(x, x, 63) // each 64-bit lane -> 0 or 1
	r := f.allocReg(0)
	hi := f.allocReg(maskOf(r))
	f.a.FmovToGpr(r, x, true)  // lane 0 -> bit 0
	f.a.NeonUmovD(hi, x, 1)    // lane 1 -> 0/1
	f.releaseF(x)
	f.a.LslImm(hi, hi, 1, true) // -> bit 1
	f.a.Orr32(r, r, hi)
	f.release(hi)
	f.pushReg(r, mtI32)
}

func (f *fn) v128SplatScalar(r Reg, size int) Reg {
	x := f.allocFReg(0)
	switch size {
	case 1:
		f.a.NeonDupGprB(x, r)
	case 2:
		f.a.NeonDupGprH(x, r)
	case 4:
		f.a.NeonDupGprS(x, r)
	case 8:
		f.a.NeonDupGprD(x, r)
	default:
		panic("arm64: invalid scalar splat width")
	}
	return x
}

func (f *fn) v128Splat(kind uint32) {
	s := f.popValue()
	switch kind {
	case 15: // i8x16.splat
		r := f.materialize(s)
		x := f.v128SplatScalar(r, 1)
		f.release(r)
		f.pushVReg(x)
	case 16: // i16x8.splat
		r := f.materialize(s)
		x := f.v128SplatScalar(r, 2)
		f.release(r)
		f.pushVReg(x)
	case 17: // i32x4.splat
		r := f.materialize(s)
		x := f.v128SplatScalar(r, 4)
		f.release(r)
		f.pushVReg(x)
	case 18: // i64x2.splat
		r := f.materialize(s)
		x := f.v128SplatScalar(r, 8)
		f.release(r)
		f.pushVReg(x)
	case 19: // f32x4.splat
		x := f.materializeF(s)
		f.a.NeonDupS(x, x)
		f.pushVReg(x)
	case 20: // f64x2.splat
		x := f.materializeF(s)
		f.a.NeonDupD(x, x)
		f.pushVReg(x)
	}
}

func (f *fn) v128ExtractLane(kind uint32, lane byte) {
	v := f.popValue()
	x := f.materializeV128(v)
	switch kind {
	case 21, 22: // i8x16.extract_lane_s/u
		r := f.allocReg(0)
		f.a.NeonUmovB(r, x, lane)
		if kind == 21 {
			f.a.Sxtb(r, r, false)
		}
		f.releaseF(x)
		f.pushReg(r, mtI32)
	case 24, 25: // i16x8.extract_lane_s/u
		r := f.allocReg(0)
		f.a.NeonUmovH(r, x, lane)
		if kind == 24 {
			f.a.Sxth(r, r, false)
		}
		f.releaseF(x)
		f.pushReg(r, mtI32)
	case 27: // i32x4.extract_lane
		r := f.allocReg(0)
		f.a.NeonUmovS(r, x, lane)
		f.releaseF(x)
		f.pushReg(r, mtI32)
	case 29: // i64x2.extract_lane
		r := f.allocReg(0)
		f.a.NeonUmovD(r, x, lane)
		f.releaseF(x)
		f.pushReg(r, mtI64)
	case 31: // f32x4.extract_lane
		if lane != 0 {
			f.a.NeonDupLaneS(x, x, lane)
		}
		f.pushFReg(x, mtF32)
	case 33: // f64x2.extract_lane
		if lane != 0 {
			f.a.NeonDupLaneD(x, x, lane)
		}
		f.pushFReg(x, mtF64)
	}
}

func (f *fn) v128ReplaceLane(kind uint32, lane byte) {
	s := f.popValue()
	v := f.popValue()
	x := f.materializeV128(v)
	switch kind {
	case 23: // i8x16.replace_lane
		r := f.materialize(s)
		f.a.NeonInsB(x, r, lane)
		f.release(r)
	case 26: // i16x8.replace_lane
		r := f.materialize(s)
		f.a.NeonInsH(x, r, lane)
		f.release(r)
	case 28: // i32x4.replace_lane
		r := f.materialize(s)
		f.a.NeonInsS(x, r, lane)
		f.release(r)
	case 30: // i64x2.replace_lane
		r := f.materialize(s)
		f.a.NeonInsD(x, r, lane)
		f.release(r)
	case 32: // f32x4.replace_lane
		f.fpinned = f.fpinned.add(x)
		sx := f.materializeF(s)
		f.fpinned = f.fpinned.remove(x)
		f.a.NeonInsLaneS(x, lane, sx)
		f.releaseF(sx)
	case 34: // f64x2.replace_lane
		f.fpinned = f.fpinned.add(x)
		sx := f.materializeF(s)
		f.fpinned = f.fpinned.remove(x)
		f.a.NeonInsLaneD(x, lane, sx)
		f.releaseF(sx)
	}
	f.pushVReg(x)
}

func (f *fn) v128Load(r *wasm.Reader) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	ea, eaOwned, _, disp := f.memAddr(off, 16, true)
	x := f.allocFReg(0)
	f.a.LdrQIdx(x, linMemReg, ea, disp)
	if eaOwned {
		f.release(ea)
	}
	f.pushVReg(x)
	return nil
}

func (f *fn) v128LoadExtend(r *wasm.Reader, sub uint32) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	ea, eaOwned, _, disp := f.memAddr(off, 8, true)
	t := f.allocReg(0)
	f.a.LoadIdx(t, linMemReg, ea, disp, 8, false, true)
	if eaOwned {
		f.release(ea)
	}
	x := f.allocFReg(0)
	f.a.FmovFromGpr(x, t, true)
	f.release(t)

	switch sub {
	case 1: // v128.load8x8_s
		f.a.NeonSxtlHfromB(x, x)
	case 2: // v128.load8x8_u
		f.a.NeonUxtlHfromB(x, x)
	case 3: // v128.load16x4_s
		f.a.NeonSxtlSfromH(x, x)
	case 4: // v128.load16x4_u
		f.a.NeonUxtlSfromH(x, x)
	case 5: // v128.load32x2_s
		f.a.NeonSxtlDfromS(x, x)
	case 6: // v128.load32x2_u
		f.a.NeonUxtlDfromS(x, x)
	default:
		panic("arm64: invalid SIMD load-extend opcode")
	}
	f.pushVReg(x)
	return nil
}

func simdLoadSplatSize(sub uint32) int {
	switch sub {
	case 7:
		return 1
	case 8:
		return 2
	case 9:
		return 4
	case 10:
		return 8
	}
	panic("arm64: invalid SIMD load-splat opcode")
}

func (f *fn) v128LoadSplat(r *wasm.Reader, sub uint32) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	size := simdLoadSplatSize(sub)
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	t := f.allocReg(0)
	f.a.LoadIdx(t, linMemReg, ea, disp, size, false, size == 8)
	if eaOwned {
		f.release(ea)
	}
	x := f.v128SplatScalar(t, size)
	f.release(t)
	f.pushVReg(x)
	return nil
}

func simdLoadZeroSize(sub uint32) int {
	switch sub {
	case 92:
		return 4
	case 93:
		return 8
	}
	panic("arm64: invalid SIMD load-zero opcode")
}

func (f *fn) v128LoadZero(r *wasm.Reader, sub uint32) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	size := simdLoadZeroSize(sub)
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	t := f.allocReg(0)
	f.a.LoadIdx(t, linMemReg, ea, disp, size, false, size == 8)
	if eaOwned {
		f.release(ea)
	}
	x := f.allocFReg(0)
	f.a.FmovFromGpr(x, t, size == 8) // FMOV S/D zeroes the rest of the vector.
	f.release(t)
	f.pushVReg(x)
	return nil
}

func (f *fn) v128Store(r *wasm.Reader) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	v := f.popValue()
	x := f.materializeV128(v)
	f.fpinned = f.fpinned.add(x)
	ea, eaOwned, _, disp := f.memAddr(off, 16, true)
	f.pinned = f.pinned.add(ea)
	f.materializePendingLoadsBeforeStore(ea, disp, 16)
	f.a.StrQIdx(linMemReg, ea, x, disp)
	f.pinned = f.pinned.remove(ea)
	f.fpinned = f.fpinned.remove(x)
	if eaOwned {
		f.release(ea)
	}
	f.releaseF(x)
	return nil
}

func simdLaneMemSize(sub uint32) int {
	switch sub {
	case 84, 88:
		return 1
	case 85, 89:
		return 2
	case 86, 90:
		return 4
	case 87, 91:
		return 8
	}
	panic("arm64: invalid SIMD lane memory opcode")
}

func (f *fn) v128LoadLane(r *wasm.Reader, sub uint32) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	lane, err := r.Byte()
	if err != nil {
		return err
	}
	size := simdLaneMemSize(sub)

	v := f.popValue()
	x := f.materializeV128(v)
	f.fpinned = f.fpinned.add(x)
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	t := f.allocReg(0)
	f.a.LoadIdx(t, linMemReg, ea, disp, size, false, size == 8)
	if eaOwned {
		f.release(ea)
	}
	f.fpinned = f.fpinned.remove(x)
	switch size {
	case 1:
		f.a.NeonInsB(x, t, lane)
	case 2:
		f.a.NeonInsH(x, t, lane)
	case 4:
		f.a.NeonInsS(x, t, lane)
	case 8:
		f.a.NeonInsD(x, t, lane)
	}
	f.release(t)
	f.pushVReg(x)
	return nil
}

func (f *fn) v128StoreLane(r *wasm.Reader, sub uint32) error {
	if _, err := r.U32(); err != nil { // align
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	lane, err := r.Byte()
	if err != nil {
		return err
	}
	size := simdLaneMemSize(sub)

	v := f.popValue()
	x := f.materializeV128(v)
	f.fpinned = f.fpinned.add(x)
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	f.pinned = f.pinned.add(ea)
	f.materializePendingLoadsBeforeStore(ea, disp, size)
	t := f.allocReg(0)
	switch size {
	case 1:
		f.a.NeonUmovB(t, x, lane)
	case 2:
		f.a.NeonUmovH(t, x, lane)
	case 4:
		f.a.NeonUmovS(t, x, lane)
	case 8:
		f.a.NeonUmovD(t, x, lane)
	}
	f.a.StoreIdx(linMemReg, ea, t, disp, size)
	f.release(t)
	f.pinned = f.pinned.remove(ea)
	f.fpinned = f.fpinned.remove(x)
	if eaOwned {
		f.release(ea)
	}
	f.releaseF(x)
	return nil
}

func (f *fn) emitFD(r *wasm.Reader) error {
	sub, err := r.U32()
	if err != nil {
		return err
	}
	switch sub {
	case 0: // v128.load
		return f.v128Load(r)
	case 1, 2, 3, 4, 5, 6: // v128.load{8x8,16x4,32x2}_{s,u}
		return f.v128LoadExtend(r, sub)
	case 7, 8, 9, 10: // v128.load{8,16,32,64}_splat
		return f.v128LoadSplat(r, sub)
	case 11: // v128.store
		return f.v128Store(r)
	case 92, 93: // v128.load{32,64}_zero
		return f.v128LoadZero(r, sub)
	case 84, 85, 86, 87: // v128.load{8,16,32,64}_lane
		return f.v128LoadLane(r, sub)
	case 88, 89, 90, 91: // v128.store{8,16,32,64}_lane
		return f.v128StoreLane(r, sub)
	case 12: // v128.const
		var b [16]byte
		for i := range b {
			v, err := r.Byte()
			if err != nil {
				return err
			}
			b[i] = v
		}
		f.v128Const(binary.LittleEndian.Uint64(b[0:8]), binary.LittleEndian.Uint64(b[8:16]))
	case 13: // i8x16.shuffle
		var lanes [16]byte
		for i := range lanes {
			lane, err := r.Byte()
			if err != nil {
				return err
			}
			if lane >= 32 {
				return fmt.Errorf("arm64: invalid i8x16.shuffle lane %d", lane)
			}
			lanes[i] = lane
		}
		f.i8x16Shuffle(lanes)
	case 14: // i8x16.swizzle
		f.i8x16Swizzle()
	case 256: // i8x16.relaxed_swizzle: deterministic raw TBL semantics.
		f.v128Bin(f.a.NeonTbl)
	case 257: // i32x4.relaxed_trunc_f32x4_s: conservative saturating choice.
		f.v128I32x4TruncSat(false, true)
	case 258: // i32x4.relaxed_trunc_f32x4_u: conservative saturating choice.
		f.v128I32x4TruncSat(false, false)
	case 259: // i32x4.relaxed_trunc_f64x2_s_zero: conservative saturating choice.
		f.v128I32x4TruncSat(true, true)
	case 260: // i32x4.relaxed_trunc_f64x2_u_zero: conservative saturating choice.
		f.v128I32x4TruncSat(true, false)
	case 261: // f32x4.relaxed_madd: deterministic FMUL + FADD choice.
		f.v128RelaxedMadd(false, false)
	case 262: // f32x4.relaxed_nmadd: deterministic FMUL then subtract from addend.
		f.v128RelaxedMadd(false, true)
	case 263: // f64x2.relaxed_madd: deterministic FMUL + FADD choice.
		f.v128RelaxedMadd(true, false)
	case 264: // f64x2.relaxed_nmadd: deterministic FMUL then subtract from addend.
		f.v128RelaxedMadd(true, true)
	case 265, 266, 267, 268: // relaxed_laneselect: deterministic bitselect choice.
		f.v128Bitselect()
	case 269: // f32x4.relaxed_min: deterministic native FMIN choice.
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFmin(dst, s1, s2, false) })
	case 270: // f32x4.relaxed_max: deterministic native FMAX choice.
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFmax(dst, s1, s2, false) })
	case 271: // f64x2.relaxed_min: deterministic native FMIN choice.
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFmin(dst, s1, s2, true) })
	case 272: // f64x2.relaxed_max: deterministic native FMAX choice.
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFmax(dst, s1, s2, true) })
	case 273: // i16x8.relaxed_q15mulr_s: deterministic raw SQRDMULH choice.
		f.v128Bin(f.a.NeonSqrdmulhH)
	case 274: // i16x8.relaxed_dot_i8x16_i7x16_s: deterministic signed scalar dot with i16 saturation.
		f.i16x8RelaxedDotI8x16I7x16S()
	case 275: // i32x4.relaxed_dot_i8x16_i7x16_add_s: deterministic signed scalar dot-add.
		f.i32x4RelaxedDotI8x16I7x16AddS()
	case 15, 16, 17, 18, 19, 20: // splat
		f.v128Splat(sub)
	case 21, 22, 24, 25, 27, 29, 31, 33: // extract_lane
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		f.v128ExtractLane(sub, lane)
	case 23, 26, 28, 30, 32, 34: // replace_lane
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		f.v128ReplaceLane(sub, lane)
	case 35: // i8x16.eq
		f.v128Bin(f.a.NeonCmeqB)
	case 36: // i8x16.ne
		f.v128BinNot(f.a.NeonCmeqB)
	case 37: // i8x16.lt_s
		f.v128SignedCmp(f.a.NeonCmgtB, true, false)
	case 38: // i8x16.lt_u
		f.v128UnsignedCmp(f.a.NeonCmhiB, true)
	case 39: // i8x16.gt_s
		f.v128Bin(f.a.NeonCmgtB)
	case 40: // i8x16.gt_u
		f.v128UnsignedCmp(f.a.NeonCmhiB, false)
	case 41: // i8x16.le_s
		f.v128SignedCmp(f.a.NeonCmgeB, true, false)
	case 42: // i8x16.le_u
		f.v128UnsignedCmp(f.a.NeonCmhsB, true)
	case 43: // i8x16.ge_s
		f.v128SignedCmp(f.a.NeonCmgeB, false, false)
	case 44: // i8x16.ge_u
		f.v128UnsignedCmp(f.a.NeonCmhsB, false)
	case 45: // i16x8.eq
		f.v128Bin(f.a.NeonCmeqH)
	case 46: // i16x8.ne
		f.v128BinNot(f.a.NeonCmeqH)
	case 47: // i16x8.lt_s
		f.v128SignedCmp(f.a.NeonCmgtH, true, false)
	case 48: // i16x8.lt_u
		f.v128UnsignedCmp(f.a.NeonCmhiH, true)
	case 49: // i16x8.gt_s
		f.v128Bin(f.a.NeonCmgtH)
	case 50: // i16x8.gt_u
		f.v128UnsignedCmp(f.a.NeonCmhiH, false)
	case 51: // i16x8.le_s
		f.v128SignedCmp(f.a.NeonCmgeH, true, false)
	case 52: // i16x8.le_u
		f.v128UnsignedCmp(f.a.NeonCmhsH, true)
	case 53: // i16x8.ge_s
		f.v128SignedCmp(f.a.NeonCmgeH, false, false)
	case 54: // i16x8.ge_u
		f.v128UnsignedCmp(f.a.NeonCmhsH, false)
	case 55: // i32x4.eq
		f.v128Bin(f.a.NeonCmeqS)
	case 56: // i32x4.ne
		f.v128BinNot(f.a.NeonCmeqS)
	case 57: // i32x4.lt_s
		f.v128SignedCmp(f.a.NeonCmgtS, true, false)
	case 58: // i32x4.lt_u
		f.v128UnsignedCmp(f.a.NeonCmhiS, true)
	case 59: // i32x4.gt_s
		f.v128Bin(f.a.NeonCmgtS)
	case 60: // i32x4.gt_u
		f.v128UnsignedCmp(f.a.NeonCmhiS, false)
	case 61: // i32x4.le_s
		f.v128SignedCmp(f.a.NeonCmgeS, true, false)
	case 62: // i32x4.le_u
		f.v128UnsignedCmp(f.a.NeonCmhsS, true)
	case 63: // i32x4.ge_s
		f.v128SignedCmp(f.a.NeonCmgeS, false, false)
	case 64: // i32x4.ge_u
		f.v128UnsignedCmp(f.a.NeonCmhsS, false)
	case 65: // f32x4.eq
		f.v128FCmp(false, vfcmpEqOQ)
	case 66: // f32x4.ne
		f.v128FCmp(false, vfcmpNeqUQ)
	case 67: // f32x4.lt
		f.v128FCmp(false, vfcmpLtOQ)
	case 68: // f32x4.gt
		f.v128FCmp(false, vfcmpGtOQ)
	case 69: // f32x4.le
		f.v128FCmp(false, vfcmpLeOQ)
	case 70: // f32x4.ge
		f.v128FCmp(false, vfcmpGeOQ)
	case 71: // f64x2.eq
		f.v128FCmp(true, vfcmpEqOQ)
	case 72: // f64x2.ne
		f.v128FCmp(true, vfcmpNeqUQ)
	case 73: // f64x2.lt
		f.v128FCmp(true, vfcmpLtOQ)
	case 74: // f64x2.gt
		f.v128FCmp(true, vfcmpGtOQ)
	case 75: // f64x2.le
		f.v128FCmp(true, vfcmpLeOQ)
	case 76: // f64x2.ge
		f.v128FCmp(true, vfcmpGeOQ)
	case 101: // i8x16.narrow_i16x8_s
		f.v128NarrowI16x8ToI8x16(true)
	case 102: // i8x16.narrow_i16x8_u
		f.v128NarrowI16x8ToI8x16(false)
	case 103: // f32x4.ceil
		f.v128FloatRound(false, roundCeil)
	case 104: // f32x4.floor
		f.v128FloatRound(false, roundFloor)
	case 105: // f32x4.trunc
		f.v128FloatRound(false, roundTrunc)
	case 106: // f32x4.nearest
		f.v128FloatRound(false, roundNearest)
	case 107: // i8x16.shl
		f.i8x16Shift(f.a.NeonUshlB, false)
	case 108: // i8x16.shr_s
		f.i8x16Shift(f.a.NeonSshrvB, true)
	case 109: // i8x16.shr_u
		f.i8x16Shift(f.a.NeonUshrvB, true)
	case 110: // i8x16.add
		f.v128Bin(f.a.NeonAddB)
	case 111: // i8x16.add_sat_s
		f.v128Bin(f.a.NeonSqaddB)
	case 112: // i8x16.add_sat_u
		f.v128Bin(f.a.NeonUqaddB)
	case 113: // i8x16.sub
		f.v128Bin(f.a.NeonSubB)
	case 114: // i8x16.sub_sat_s
		f.v128Bin(f.a.NeonSqsubB)
	case 115: // i8x16.sub_sat_u
		f.v128Bin(f.a.NeonUqsubB)
	case 116: // f64x2.ceil
		f.v128FloatRound(true, roundCeil)
	case 117: // f64x2.floor
		f.v128FloatRound(true, roundFloor)
	case 118: // i8x16.min_s
		f.v128Bin(f.a.NeonSminB)
	case 119: // i8x16.min_u
		f.v128Bin(f.a.NeonUminB)
	case 120: // i8x16.max_s
		f.v128Bin(f.a.NeonSmaxB)
	case 121: // i8x16.max_u
		f.v128Bin(f.a.NeonUmaxB)
	case 122: // f64x2.trunc
		f.v128FloatRound(true, roundTrunc)
	case 123: // i8x16.avgr_u
		f.v128Bin(f.a.NeonUrhaddB)
	case 124: // i16x8.extadd_pairwise_i8x16_s
		f.i16x8ExtaddPairwiseI8x16(true)
	case 125: // i16x8.extadd_pairwise_i8x16_u
		f.i16x8ExtaddPairwiseI8x16(false)
	case 126: // i32x4.extadd_pairwise_i16x8_s
		f.i32x4ExtaddPairwiseI16x8(true)
	case 127: // i32x4.extadd_pairwise_i16x8_u
		f.i32x4ExtaddPairwiseI16x8(false)
	case 130: // i16x8.q15mulr_sat_s
		f.i16x8Q15mulrSatS()
	case 133: // i16x8.narrow_i32x4_s
		f.v128NarrowI32x4ToI16x8(true)
	case 134: // i16x8.narrow_i32x4_u
		f.v128NarrowI32x4ToI16x8(false)
	case 135: // i16x8.extend_low_i8x16_s
		f.i16x8ExtendI8x16(true, false)
	case 136: // i16x8.extend_high_i8x16_s
		f.i16x8ExtendI8x16(true, true)
	case 137: // i16x8.extend_low_i8x16_u
		f.i16x8ExtendI8x16(false, false)
	case 138: // i16x8.extend_high_i8x16_u
		f.i16x8ExtendI8x16(false, true)
	case 139: // i16x8.shl
		f.i16x8Shift(f.a.NeonUshlH, false)
	case 140: // i16x8.shr_s
		f.i16x8Shift(f.a.NeonSshrvH, true)
	case 141: // i16x8.shr_u
		f.i16x8Shift(f.a.NeonUshrvH, true)
	case 142: // i16x8.add
		f.v128Bin(f.a.NeonAddH)
	case 143: // i16x8.add_sat_s
		f.v128Bin(f.a.NeonSqaddH)
	case 144: // i16x8.add_sat_u
		f.v128Bin(f.a.NeonUqaddH)
	case 145: // i16x8.sub
		f.v128Bin(f.a.NeonSubH)
	case 146: // i16x8.sub_sat_s
		f.v128Bin(f.a.NeonSqsubH)
	case 147: // i16x8.sub_sat_u
		f.v128Bin(f.a.NeonUqsubH)
	case 148: // f64x2.nearest
		f.v128FloatRound(true, roundNearest)
	case 149: // i16x8.mul
		f.v128Bin(f.a.NeonMulH)
	case 150: // i16x8.min_s
		f.v128Bin(f.a.NeonSminH)
	case 151: // i16x8.min_u
		f.v128Bin(f.a.NeonUminH)
	case 152: // i16x8.max_s
		f.v128Bin(f.a.NeonSmaxH)
	case 153: // i16x8.max_u
		f.v128Bin(f.a.NeonUmaxH)
	case 155: // i16x8.avgr_u
		f.v128Bin(f.a.NeonUrhaddH)
	case 156: // i16x8.extmul_low_i8x16_s
		f.i16x8ExtmulI8x16(true, false)
	case 157: // i16x8.extmul_high_i8x16_s
		f.i16x8ExtmulI8x16(true, true)
	case 158: // i16x8.extmul_low_i8x16_u
		f.i16x8ExtmulI8x16(false, false)
	case 159: // i16x8.extmul_high_i8x16_u
		f.i16x8ExtmulI8x16(false, true)
	case 167: // i32x4.extend_low_i16x8_s
		f.i32x4ExtendI16x8(true, false)
	case 168: // i32x4.extend_high_i16x8_s
		f.i32x4ExtendI16x8(true, true)
	case 169: // i32x4.extend_low_i16x8_u
		f.i32x4ExtendI16x8(false, false)
	case 170: // i32x4.extend_high_i16x8_u
		f.i32x4ExtendI16x8(false, true)
	case 171: // i32x4.shl
		f.i32x4Shift(f.a.NeonUshlS, false)
	case 172: // i32x4.shr_s
		f.i32x4Shift(f.a.NeonSshrvS, true)
	case 173: // i32x4.shr_u
		f.i32x4Shift(f.a.NeonUshrvS, true)
	case 199: // i64x2.extend_low_i32x4_s
		f.i64x2ExtendI32x4(true, false)
	case 200: // i64x2.extend_high_i32x4_s
		f.i64x2ExtendI32x4(true, true)
	case 201: // i64x2.extend_low_i32x4_u
		f.i64x2ExtendI32x4(false, false)
	case 202: // i64x2.extend_high_i32x4_u
		f.i64x2ExtendI32x4(false, true)
	case 203: // i64x2.shl
		f.i64x2Shift(f.a.NeonUshlD, false)
	case 204: // i64x2.shr_s
		f.i64x2ShrS()
	case 205: // i64x2.shr_u
		f.i64x2Shift(f.a.NeonUshrvD, true)
	case 174: // i32x4.add
		f.v128Bin(f.a.NeonAddS)
	case 177: // i32x4.sub
		f.v128Bin(f.a.NeonSubS)
	case 181: // i32x4.mul
		f.v128Bin(f.a.NeonMulS)
	case 182: // i32x4.min_s
		f.v128Bin(f.a.NeonSminS)
	case 183: // i32x4.min_u
		f.v128Bin(f.a.NeonUminS)
	case 184: // i32x4.max_s
		f.v128Bin(f.a.NeonSmaxS)
	case 185: // i32x4.max_u
		f.v128Bin(f.a.NeonUmaxS)
	case 186: // i32x4.dot_i16x8_s
		f.i32x4DotI16x8S()
	case 188: // i32x4.extmul_low_i16x8_s
		f.i32x4ExtmulI16x8(true, false)
	case 189: // i32x4.extmul_high_i16x8_s
		f.i32x4ExtmulI16x8(true, true)
	case 190: // i32x4.extmul_low_i16x8_u
		f.i32x4ExtmulI16x8(false, false)
	case 191: // i32x4.extmul_high_i16x8_u
		f.i32x4ExtmulI16x8(false, true)
	case 206: // i64x2.add
		f.v128Bin(f.a.NeonAddD)
	case 209: // i64x2.sub
		f.v128Bin(f.a.NeonSubD)
	case 213: // i64x2.mul
		f.i64x2Mul()
	case 220: // i64x2.extmul_low_i32x4_s
		f.i64x2ExtmulI32x4(true, false)
	case 221: // i64x2.extmul_high_i32x4_s
		f.i64x2ExtmulI32x4(true, true)
	case 222: // i64x2.extmul_low_i32x4_u
		f.i64x2ExtmulI32x4(false, false)
	case 223: // i64x2.extmul_high_i32x4_u
		f.i64x2ExtmulI32x4(false, true)
	case 214: // i64x2.eq
		f.v128Bin(f.a.NeonCmeqD)
	case 215: // i64x2.ne
		f.v128BinNot(f.a.NeonCmeqD)
	case 216: // i64x2.lt_s
		f.i64x2SignedCmp(condL)
	case 217: // i64x2.gt_s
		f.i64x2SignedCmp(condG)
	case 218: // i64x2.le_s
		f.i64x2SignedCmp(condLE)
	case 219: // i64x2.ge_s
		f.i64x2SignedCmp(condGE)
	case 224: // f32x4.abs
		f.v128IntegerAbs(func(dst, src Reg) { f.a.NeonFabs(dst, src, false) })
	case 225: // f32x4.neg
		f.v128IntegerAbs(func(dst, src Reg) { f.a.NeonFneg(dst, src, false) })
	case 227: // f32x4.sqrt
		f.v128IntegerAbs(func(dst, src Reg) { f.a.NeonFsqrt(dst, src, false) })
	case 228: // f32x4.add
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFadd(dst, s1, s2, false) })
	case 229: // f32x4.sub
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFsub(dst, s1, s2, false) })
	case 230: // f32x4.mul
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFmul(dst, s1, s2, false) })
	case 231: // f32x4.div
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFdiv(dst, s1, s2, false) })
	case 232: // f32x4.min
		f.v128FloatMinMax(false, false)
	case 233: // f32x4.max
		f.v128FloatMinMax(false, true)
	case 234: // f32x4.pmin: deterministic pseudo-min with first operand winning equal/NaN-second lanes.
		f.v128FloatPMinMax(false, false)
	case 235: // f32x4.pmax: deterministic pseudo-max with first operand winning equal/NaN-second lanes.
		f.v128FloatPMinMax(false, true)
	case 236: // f64x2.abs
		f.v128IntegerAbs(func(dst, src Reg) { f.a.NeonFabs(dst, src, true) })
	case 237: // f64x2.neg
		f.v128IntegerAbs(func(dst, src Reg) { f.a.NeonFneg(dst, src, true) })
	case 239: // f64x2.sqrt
		f.v128IntegerAbs(func(dst, src Reg) { f.a.NeonFsqrt(dst, src, true) })
	case 240: // f64x2.add
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFadd(dst, s1, s2, true) })
	case 241: // f64x2.sub
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFsub(dst, s1, s2, true) })
	case 242: // f64x2.mul
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFmul(dst, s1, s2, true) })
	case 243: // f64x2.div
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.NeonFdiv(dst, s1, s2, true) })
	case 244: // f64x2.min
		f.v128FloatMinMax(true, false)
	case 245: // f64x2.max
		f.v128FloatMinMax(true, true)
	case 246: // f64x2.pmin: deterministic pseudo-min with first operand winning equal/NaN-second lanes.
		f.v128FloatPMinMax(true, false)
	case 247: // f64x2.pmax: deterministic pseudo-max with first operand winning equal/NaN-second lanes.
		f.v128FloatPMinMax(true, true)
	case 248: // i32x4.trunc_sat_f32x4_s
		f.v128I32x4TruncSat(false, true)
	case 249: // i32x4.trunc_sat_f32x4_u
		f.v128I32x4TruncSat(false, false)
	case 250: // f32x4.convert_i32x4_s
		f.v128I32x4ConvertToFloat(false, true)
	case 251: // f32x4.convert_i32x4_u
		f.v128I32x4ConvertToFloat(false, false)
	case 252: // i32x4.trunc_sat_f64x2_s_zero
		f.v128I32x4TruncSat(true, true)
	case 253: // i32x4.trunc_sat_f64x2_u_zero
		f.v128I32x4TruncSat(true, false)
	case 254: // f64x2.convert_low_i32x4_s
		f.v128I32x4ConvertToFloat(true, true)
	case 255: // f64x2.convert_low_i32x4_u
		f.v128I32x4ConvertToFloat(true, false)
	case 83: // v128.any_true
		f.v128AnyTrue()
	case 94: // f32x4.demote_f64x2_zero
		f.v128DemoteF64x2Zero()
	case 95: // f64x2.promote_low_f32x4
		f.v128PromoteLowF32x4()
	case 99: // i8x16.all_true
		f.i8x16AllTrue()
	case 100: // i8x16.bitmask
		f.i8x16Bitmask()
	case 131: // i16x8.all_true
		f.i16x8AllTrue()
	case 132: // i16x8.bitmask
		f.i16x8Bitmask()
	case 163: // i32x4.all_true
		f.i32x4AllTrue()
	case 164: // i32x4.bitmask
		f.i32x4Bitmask()
	case 195: // i64x2.all_true
		f.i64x2AllTrue()
	case 196: // i64x2.bitmask
		f.i64x2Bitmask()
	case 96: // i8x16.abs
		f.v128IntegerAbs(f.a.NeonAbsB)
	case 97: // i8x16.neg
		f.v128IntegerNeg(f.a.NeonNegB)
	case 98: // i8x16.popcnt
		f.i8x16Popcnt()
	case 128: // i16x8.abs
		f.v128IntegerAbs(f.a.NeonAbsH)
	case 129: // i16x8.neg
		f.v128IntegerNeg(f.a.NeonNegH)
	case 160: // i32x4.abs
		f.v128IntegerAbs(f.a.NeonAbsS)
	case 161: // i32x4.neg
		f.v128IntegerNeg(f.a.NeonNegS)
	case 192: // i64x2.abs
		f.i64x2Abs()
	case 193: // i64x2.neg
		f.v128IntegerNeg(f.a.NeonNegD)
	case 77: // v128.not
		f.v128UnaryNot()
	case 78: // v128.and
		f.v128Bin(f.a.NeonAnd16b)
	case 79: // v128.andnot (a &^ b)
		// NEON BIC Vd.16b, Vn.16b, Vm.16b computes Vn & ~Vm directly, so the amd64
		// not+and emulation collapses to a single BIC. Structure kept for parity.
		b := f.popValue()
		a := f.popValue()
		xa := f.materializeV128(a)
		f.fpinned = f.fpinned.add(xa)
		xb := f.materializeV128(b)
		m := f.allocFReg(maskOf(xa, xb))
		f.a.NeonCmeqB(m, m, m)
		f.a.NeonEor16b(xb, xb, m)
		f.releaseF(m)
		f.fpinned = f.fpinned.remove(xa)
		f.a.NeonAnd16b(xa, xa, xb)
		f.releaseF(xb)
		f.pushVReg(xa)
	case 80: // v128.or
		f.v128Bin(f.a.NeonOrr16b)
	case 81: // v128.xor
		f.v128Bin(f.a.NeonEor16b)
	case 82: // v128.bitselect: (a & mask) | (b & ~mask)
		f.v128Bitselect()
	default:
		return fmt.Errorf("arm64: unsupported 0xFD opcode %d", sub)
	}
	return nil
}
