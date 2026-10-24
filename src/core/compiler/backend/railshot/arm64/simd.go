//go:build arm64

package arm64

import (
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

type v128ConstReg struct {
	lo, hi uint64
	reg    Reg
}

const maxV128Consts = 4

func (f *fn) v128UnaryNot(r *wasm.Reader) error { return f.v128Unary(r, f.a.NeonNot16b) }

func (f *fn) v128IntegerNeg(r *wasm.Reader, op func(dst, src Reg)) error {
	return f.v128Unary(r, op)
}

func (f *fn) v128IntegerAbs(r *wasm.Reader, op func(dst, src Reg)) error {
	return f.v128Unary(r, op)
}

func (f *fn) v128FloatRound(r *wasm.Reader, f64 bool, mode byte) error {
	return f.v128Unary(r, func(dst, src Reg) { f.a.NeonFrint(dst, src, f64, mode) })
}

func (f *fn) i8x16Popcnt(r *wasm.Reader) error { return f.v128Unary(r, f.a.NeonCntB) }

var (
	i8x16Rotate16 = [16]byte{2, 3, 0, 1, 6, 7, 4, 5, 10, 11, 8, 9, 14, 15, 12, 13}
	i8x16Rotate8  = [16]byte{1, 2, 3, 0, 5, 6, 7, 4, 9, 10, 11, 8, 13, 14, 15, 12}
	i8x16Zip1D    = [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 16, 17, 18, 19, 20, 21, 22, 23}
	i8x16Zip2D    = [16]byte{8, 9, 10, 11, 12, 13, 14, 15, 24, 25, 26, 27, 28, 29, 30, 31}
	i8x16Zip1S    = [16]byte{0, 1, 2, 3, 16, 17, 18, 19, 4, 5, 6, 7, 20, 21, 22, 23}
	i8x16Zip2S    = [16]byte{8, 9, 10, 11, 24, 25, 26, 27, 12, 13, 14, 15, 28, 29, 30, 31}
)

// v128FloatMinMax uses AArch64's IEEE-propagating FMIN/FMAX directly. Unlike
// x86 MINPS/MAXPS, these instructions return NaN when either operand is NaN and
// select -0 for min / +0 for max, exactly the deterministic parts of Wasm's
// semantics. Wasm permits any quiet arithmetic NaN payload, so canonicalizing
// every NaN lane in software only adds latency.
func (f *fn) v128FloatMinMax(r *wasm.Reader, f64, isMax bool) error {
	if isMax {
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFmax(dst, s1, s2, f64) })
	}
	return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFmin(dst, s1, s2, f64) })
}

func (f *fn) v128FloatPMinMax(r *wasm.Reader, f64, isMax bool) error {
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
	return nil
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

// v128I32x4TruncSat is a unary v128 op (one v128 in, one v128 out): for f32
// sources a single FCVTZ{S,U}; for f64 sources FCVTZ{S,U}.2D to saturating i64
// lanes followed by a saturating narrow to the two low i32 lanes (clearing the
// high half). FCVTZ supplies Wasm's required NaN->0 and int-min/max overflow
// saturation.
func (f *fn) v128I32x4TruncSat(r *wasm.Reader, f64src, signed bool) error {
	var op func(dst, src Reg)
	switch {
	case !f64src && signed:
		op = f.a.NeonFcvtzsSfromS
	case !f64src:
		op = f.a.NeonFcvtzuSfromS
	case signed:
		op = func(dst, src Reg) { f.a.NeonFcvtzsDfromD(dst, src); f.a.NeonSqxtnSfromD(dst, dst) }
	default:
		op = func(dst, src Reg) { f.a.NeonFcvtzuDfromD(dst, src); f.a.NeonUqxtnSfromD(dst, dst) }
	}
	return f.v128Unary(r, op)
}

// v128DemoteF64x2Zero is unary: FCVTN.2S narrows the two f64 lanes into the two
// low f32 lanes and clears the high 64 bits (lanes 2,3 = +0), matching
// demote_f64x2_zero. It reads src before writing dst, so it sinks through the
// unary path; no explicit zeroing of the high half is needed because the .2S
// destination form clears bits[127:64].
func (f *fn) v128DemoteF64x2Zero(r *wasm.Reader) error {
	return f.v128Unary(r, f.a.NeonFcvtnSfromD)
}

// v128PromoteLowF32x4 is unary: FCVTL.2D widens the two low f32 lanes to two f64
// lanes. Reads src before writing dst; sinks through the unary path.
func (f *fn) v128PromoteLowF32x4(r *wasm.Reader) error {
	return f.v128Unary(r, f.a.NeonFcvtlDfromS)
}

// v128I32x4ConvertToFloat is unary: for f32 a single SCVTF/UCVTF; for f64 a
// widen (SXTL/UXTL of the two low i32 lanes) followed by SCVTF/UCVTF.2D. Reads
// src before the first write, so it sinks through the unary path.
func (f *fn) v128I32x4ConvertToFloat(r *wasm.Reader, f64dst, signed bool) error {
	var op func(dst, src Reg)
	switch {
	case !f64dst && signed:
		op = f.a.NeonScvtfSfromS
	case !f64dst:
		op = f.a.NeonUcvtfSfromS
	case signed:
		op = func(dst, src Reg) { f.a.NeonSxtlDfromS(dst, src); f.a.NeonScvtfDfromD(dst, dst) }
	default:
		op = func(dst, src Reg) { f.a.NeonUxtlDfromS(dst, src); f.a.NeonUcvtfDfromD(dst, dst) }
	}
	return f.v128Unary(r, op)
}

func (f *fn) v128ShiftLegacy(op func(dst, s1, s2 Reg), opImm func(dst, src Reg, shift uint8), countMask int32, laneSize int, right bool) error {
	countElem := f.popValue()
	if countElem.st.kind == stConst {
		value := f.popValue()
		x := f.materializeV128(value)
		if shift := uint8(countElem.st.cval & int64(countMask)); shift != 0 {
			opImm(x, x, shift)
		}
		f.stats.peep("simd-shift-imm")
		f.pushVReg(x)
		return nil
	}
	count := f.materialize(countElem)
	f.andImm(count, int64(countMask), false)
	if right {
		f.a.Sub64(count, ZR, count)
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
	return nil
}
func (f *fn) i8x16Shift(r *wasm.Reader, op func(dst, s1, s2 Reg), opImm func(dst, src Reg, shift uint8), right bool) error {
	return f.v128Shift(r, op, opImm, 7, 1, right, false)
}

func (f *fn) i64x2Shift(r *wasm.Reader, op func(dst, s1, s2 Reg), opImm func(dst, src Reg, shift uint8), right bool) error {
	return f.v128Shift(r, op, opImm, 63, 8, right, false)
}

// i64x2.shr_s uses the same packed SSHL path as every other vector shift: SSHL.2D
// with a negated, splatted count performs an arithmetic (sign-replicating) 64-bit
// lane shift-right (see v128Shift / dispatch case 204). No lane-by-lane GPR
// round-trip is needed — SSHL.2D exists on the base NEON profile.

func (f *fn) i64x2Abs(r *wasm.Reader) error { return f.v128Unary(r, f.a.NeonAbsD) }

// i64x2Mul: NEON has no MUL.2D, so use the standard widening recombine (LLVM/V8
// sequence) entirely in the vector unit — no GPR round-trips. For each 64-bit lane
// with a = aHi·2^32+aLo, b = bHi·2^32+bLo (aLo/bLo the low 32 bits), the product
// mod 2^64 is aLo·bLo + ((aLo·bHi + aHi·bLo) << 32); the aHi·bHi·2^64 term vanishes.
//
//	t = rev64(b)·a  (32-bit lanes)   -> low32(aLo·bHi), low32(aHi·bLo) per half
//	t = uaddlp(t)                    -> (aLo·bHi + aHi·bLo) widened per 64-bit lane
//	t = t << 32
//	t += xtn(a) · xtn(b)             -> UMLAL of the packed low-32 halves (aLo·bLo)
//
// Truncating the cross products to 32 bits before summing is exact: only the low
// 32 bits of the cross sum survive the <<32 mod 2^64.
func (f *fn) i64x2Mul(r *wasm.Reader) error {
	out := f.allocFReg(0)
	f.i64x2MulInto(out)
	f.pushVReg(out)
	return nil
}

// i64x2MulInto computes the 64-bit lane products into dst. The high cross-product
// term (t = (aLo·bHi + aHi·bLo) << 32) is formed in a temporary, the low product
// (aLo·bLo) widened into another, and the two are summed into dst LAST — so dst is
// written only after both sources are dead, making the write safe even when dst
// aliases a source register (the `local.set $a (mul $a $b)` accumulator). Splitting
// the closing UMLAL into UMULL + ADD.2d (vs accumulating in place) is what frees the
// destination, letting the result sink into the pinned local with no extra copy.
func (f *fn) i64x2MulInto(dst Reg) {
	b := f.popValue()
	a := f.popValue()
	xa, oa := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb, ob := f.operandRegV128(b)
	f.fpinned = f.fpinned.add(xb)

	t := f.allocFReg(maskOf(xa, xb))
	f.fpinned = f.fpinned.add(t)
	f.a.NeonRev64S(t, xb)
	f.a.NeonMulS(t, t, xa)
	f.a.NeonUaddlpDfromS(t, t)
	f.a.NeonShlD(t, t, 32) // t = (aLo·bHi + aHi·bLo) << 32

	aLo := f.allocFReg(maskOf(xa, xb, t))
	bLo := f.allocFReg(maskOf(xa, xb, t, aLo))
	f.a.NeonXtnSfromD(aLo, xa) // last use of xa
	f.a.NeonXtnSfromD(bLo, xb) // last use of xb
	// UMULL reads both low halves before writing its destination. Reuse aLo
	// after that last input read so pinned vector kernels need one less scratch.
	f.a.NeonUmullDfromS(aLo, aLo, bLo) // aLo = widened low product
	f.releaseF(bLo)
	f.fpinned = f.fpinned.remove(t)
	f.a.NeonAddD(dst, t, aLo) // dst written last; reads only t and aLo
	f.releaseF(aLo)
	f.releaseF(t)

	f.fpinned = f.fpinned.remove(xa).remove(xb)
	if ob {
		f.releaseF(xb)
	}
	if oa {
		f.releaseF(xa)
	}
}

func (f *fn) i16x8ExtendI8x16(r *wasm.Reader, signed, high bool) error {
	var op func(dst, src Reg)
	switch {
	case signed && high:
		op = f.a.NeonSxtl2HfromB
	case signed:
		op = f.a.NeonSxtlHfromB
	case high:
		op = f.a.NeonUxtl2HfromB
	default:
		op = f.a.NeonUxtlHfromB
	}
	return f.v128Unary(r, op)
}

func (f *fn) i16x8ExtaddPairwiseI8x16(r *wasm.Reader, signed bool) error {
	if signed {
		return f.v128Unary(r, f.a.NeonSaddlpHfromB)
	}
	return f.v128Unary(r, f.a.NeonUaddlpHfromB)
}

func (f *fn) i32x4ExtendI16x8(r *wasm.Reader, signed, high bool) error {
	var op func(dst, src Reg)
	switch {
	case signed && high:
		op = f.a.NeonSxtl2SfromH
	case signed:
		op = f.a.NeonSxtlSfromH
	case high:
		op = f.a.NeonUxtl2SfromH
	default:
		op = f.a.NeonUxtlSfromH
	}
	return f.v128Unary(r, op)
}

// i32x4ExtmulI16x8 is the 16->32 widening-multiply twin of i16x8ExtmulI8x16; see
// that function for the aliasing/sink rationale.
func (f *fn) i32x4ExtmulI16x8(r *wasm.Reader, signed, high bool) error {
	var op func(dst, s1, s2 Reg)
	switch {
	case signed && high:
		op = f.a.NeonSmull2SfromH
	case signed:
		op = f.a.NeonSmullSfromH
	case high:
		op = f.a.NeonUmull2SfromH
	default:
		op = f.a.NeonUmullSfromH
	}
	return f.v128Bin(r, op)
}

func (f *fn) i64x2ExtendI32x4(r *wasm.Reader, signed, high bool) error {
	var op func(dst, src Reg)
	switch {
	case signed && high:
		op = f.a.NeonSxtl2DfromS
	case signed:
		op = f.a.NeonSxtlDfromS
	case high:
		op = f.a.NeonUxtl2DfromS
	default:
		op = f.a.NeonUxtlDfromS
	}
	return f.v128Unary(r, op)
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

// i16x8Q15mulrSatS lowers directly to a single SQRDMULH. AArch64's saturating
// rounding doubling multiply-high computes signed_saturate((a*b + 0x4000) >> 15)
// with an infinite-precision intermediate, so the doubling of INT16_MIN*INT16_MIN
// saturates to 0x7fff exactly as Wasm requires (unlike x86 PMULHRSW, which wraps
// to 0x8000 — the very reason the relaxed variant exists). No software fixup for
// the overflow lane is needed; the earlier CMEQ/AND/ANDN/ORR dance was redundant.
func (f *fn) i16x8Q15mulrSatS(r *wasm.Reader) error {
	return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonSqrdmulhH(dst, s1, s2) })
}

// v128BinNot lowers `ne` = not(eq): the compare op, then a full-width NOT.
func (f *fn) v128BinNot(r *wasm.Reader, op func(dst, s1, s2 Reg)) error {
	if f.opt(optV128DirectResults) {
		f.v128BinDirect(op, false, true)
		return nil
	}
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.remove(xa)
	op(xa, xa, xb)
	f.releaseF(xb)
	f.a.NeonNot16b(xa, xa)
	f.pushVReg(xa)
	return nil
}

func (f *fn) i64x2SignedCmp(r *wasm.Reader, cc Cond) error {
	var op func(dst, s1, s2 Reg)
	var swap bool
	switch cc {
	case condL:
		op, swap = f.a.NeonCmgtD, true
	case condG:
		op, swap = f.a.NeonCmgtD, false
	case condLE:
		op, swap = f.a.NeonCmgeD, true
	case condGE:
		op, swap = f.a.NeonCmgeD, false
	default:
		panic("arm64: unsupported i64x2 signed compare")
	}
	if f.opt(optV128DirectResults) {
		f.v128BinDirect(op, swap, false)
		return nil
	}
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
	f.fpinned = f.fpinned.remove(xb)
	f.releaseF(xb)
	f.pushVReg(xa)
	return nil
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

func (f *fn) v128FCmp(r *wasm.Reader, f64 bool, pred byte) error {
	if pred == vfcmpNeqUQ {
		return f.v128BinNot(r, func(dst, s1, s2 Reg) { f.a.NeonFcmp(dst, s1, s2, f64, vfcmpEqOQ) })
	}
	return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFcmp(dst, s1, s2, f64, pred) })
}

// tryV128NotAnd selects `a & ~b` from adjacent v128.not + v128.and and lowers
// it to NEON BIC. Sources may alias the chosen destination: the three-register
// instruction reads both before writing it.
func (f *fn) tryV128NotAnd(r *wasm.Reader) bool {
	if !f.opt(optSIMDSuperopt) || !matchNextSIMDOp(r, 78) {
		return false
	}
	b := f.popValue()
	a := f.popValue()
	sa, oa := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(sa)
	sb, ob := f.operandRegV128(b)
	f.fpinned = f.fpinned.remove(sa)
	dst := sa
	if !oa {
		if ob {
			dst = sb
		} else {
			dst = f.allocFReg(maskOf(sa, sb))
		}
	}
	f.a.NeonAndn16b(dst, sa, sb)
	if oa && dst != sa {
		f.releaseF(sa)
	}
	if ob && dst != sb {
		f.releaseF(sb)
	}
	f.pushVReg(dst)
	f.stats.peep("simd-not-and")
	return true
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

// i64x2AllTrue reports whether both 64-bit lanes are non-zero. UMINV.2d does not
// exist, so it compares each lane to zero (CMEQ.2d gives an all-ones lane exactly
// where the source lane was zero), then folds the two 64-bit lanes into a GPR and
// tests for zero. Unlike the generic v128AllTrue (CMEQ + UMAXV.16b + UMOV.B), this
// uses a single cross-lane extract (UMOV lane 1) plus an FMOV of lane 0 — the
// NEON cross-lane reduction port, not raw instruction count, is the M4 bottleneck
// for this loop, and halving the cross-lane ops brings it to parity.
func (f *fn) i64x2AllTrue() {
	v := f.popValue()
	x := f.materializeV128(v)
	r := f.allocReg(0)
	hi := f.allocReg(maskOf(r))
	f.a.FmovToGpr(r, x, true) // lane 0 → r
	f.a.NeonUmovD(hi, x, 1)   // lane 1 → hi
	f.releaseF(x)
	// all_true iff both lanes non-zero. The two lane tests are kept independent
	// (parallel CMP/CSET, then AND) rather than fused via CCMP: on M4 the two
	// extractions already sit on the critical path, and a CCMP chain would only
	// serialize the second compare behind the first.
	f.a.CmpImm64(r, 0)
	f.a.Cset32(r, condNE)
	f.a.CmpImm64(hi, 0)
	f.a.Cset32(hi, condNE)
	f.a.And32(r, r, hi)
	f.release(hi)
	f.pushReg(r, mtI32)
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
	f.a.FmovToGpr(r, x, true) // lane 0 -> bit 0
	f.a.NeonUmovD(hi, x, 1)   // lane 1 -> 0/1
	f.releaseF(x)
	f.a.LslImm(hi, hi, 1, true) // -> bit 1
	f.a.Orr32(r, r, hi)
	f.release(hi)
	f.pushReg(r, mtI32)
}

func (f *fn) v128LoadExtend(r *wasm.Reader, sub uint32) error {
	memoryIndex, off, err := f.readMemArg(r)
	if err != nil {
		return err
	}
	base, ea, releaseBase, eaOwned, _, disp := f.memAddrAt(memoryIndex, off, 8)
	t := f.allocReg(maskOf(base, ea))
	f.a.LoadIdx(t, base, ea, disp, 8, false, true)
	if releaseBase {
		f.release(base)
	}
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
	memoryIndex, off, err := f.readMemArg(r)
	if err != nil {
		return err
	}
	size := simdLoadSplatSize(sub)
	base, ea, releaseBase, eaOwned, _, disp := f.memAddrAt(memoryIndex, off, size)
	t := f.allocReg(maskOf(base, ea))
	f.a.LoadIdx(t, base, ea, disp, size, false, size == 8)
	if releaseBase {
		f.release(base)
	}
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
	memoryIndex, off, err := f.readMemArg(r)
	if err != nil {
		return err
	}
	size := simdLoadZeroSize(sub)
	base, ea, releaseBase, eaOwned, _, disp := f.memAddrAt(memoryIndex, off, size)
	t := f.allocReg(maskOf(base, ea))
	f.a.LoadIdx(t, base, ea, disp, size, false, size == 8)
	if releaseBase {
		f.release(base)
	}
	if eaOwned {
		f.release(ea)
	}
	x := f.allocFReg(0)
	f.a.FmovFromGpr(x, t, size == 8) // FMOV S/D zeroes the rest of the vector.
	f.release(t)
	f.pushVReg(x)
	return nil
}

func (f *fn) v128LoadLane(r *wasm.Reader, sub uint32) error {
	memoryIndex, off, err := f.readMemArg(r)
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
	base, ea, releaseBase, eaOwned, _, disp := f.memAddrAt(memoryIndex, off, size)
	t := f.allocReg(maskOf(base, ea))
	f.a.LoadIdx(t, base, ea, disp, size, false, size == 8)
	if releaseBase {
		f.release(base)
	}
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
