package amd64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) materializeV128(e *elem) Reg {
	if e.isDeferred() {
		panic("amd64: deferred v128 op not supported")
	}
	switch e.st.kind {
	case stReg:
		return e.st.reg
	case stSlot:
		x := f.allocFReg(0)
		f.a.VMovdquLoadDisp(x, RSP, f.spillOff(e.st.slot))
		f.occupyF(e, x)
		return x
	case stLocalRef:
		x := f.allocFReg(0)
		f.a.VMovdquLoadDisp(x, RSP, f.localOff(e.st.idx))
		f.occupyF(e, x)
		return x
	case stLocalReg:
		x := f.allocFReg(0)
		f.a.VMovdquLoadDisp(x, RSP, f.localOff(e.st.idx))
		f.occupyF(e, x)
		return x
	}
	panic("amd64: cannot materialize v128 storage")
}

func (f *fn) pushVReg(r Reg) *elem {
	e := f.pushValue(storage{kind: stReg, typ: mtV128, reg: r})
	f.fregUser[r] = e
	return e
}

func (f *fn) v128ConstReg(lo, hi uint64) Reg {
	x := f.allocFReg(0)
	if lo == 0 && hi == 0 {
		f.a.VPxor(x, x, x)
		return x
	}
	slot := f.allocSpillSlots(2)
	t := f.allocReg(0)
	f.a.MovImm64(t, lo)
	f.a.Store64(RSP, f.spillOff(slot), t)
	f.a.MovImm64(t, hi)
	f.a.Store64(RSP, f.spillOff(slot)+8, t)
	f.release(t)
	f.a.VMovdquLoadDisp(x, RSP, f.spillOff(slot))
	return x
}

func (f *fn) v128Const(lo, hi uint64) {
	f.pushVReg(f.v128ConstReg(lo, hi))
}

func (f *fn) v128UnaryNot() {
	a := f.popValue()
	x := f.materializeV128(a)
	m := f.allocFReg(maskOf(x))
	f.a.VPcmpeqb(m, m, m)
	f.a.VPxor(x, x, m)
	f.releaseF(m)
	f.pushVReg(x)
}

func (f *fn) v128IntegerNeg(op func(dst, s1, s2 Reg)) {
	a := f.popValue()
	x := f.materializeV128(a)
	z := f.allocFReg(maskOf(x))
	f.a.VPxor(z, z, z)
	op(x, z, x)
	f.releaseF(z)
	f.pushVReg(x)
}

func (f *fn) v128IntegerAbs(op func(dst, src Reg)) {
	a := f.popValue()
	x := f.materializeV128(a)
	op(x, x)
	f.pushVReg(x)
}

func (f *fn) i8x16Popcnt() {
	v := f.popValue()
	x := f.materializeV128(v)
	f.fpinned = f.fpinned.add(x)

	high := f.allocFReg(0)
	f.fpinned = f.fpinned.add(high)
	f.a.VPsrlwImm(high, x, 4)

	mask := f.v128ConstReg(0x0f0f0f0f0f0f0f0f, 0x0f0f0f0f0f0f0f0f)
	f.fpinned = f.fpinned.add(mask)
	lut := f.v128ConstReg(0x0302020102010100, 0x0403030203020201)

	f.a.VPand(x, x, mask)
	f.a.VPand(high, high, mask)
	f.fpinned = f.fpinned.remove(mask)
	f.releaseF(mask)

	f.a.VPshufb(x, lut, x)
	f.a.VPshufb(high, lut, high)
	f.releaseF(lut)
	f.a.VPaddb(x, x, high)

	f.fpinned = f.fpinned.remove(x).remove(high)
	f.releaseF(high)
	f.pushVReg(x)
}

func (f *fn) v128Bin(op func(dst, s1, s2 Reg)) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.remove(xa)
	op(xa, xa, xb)
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) v128Shift(op func(dst, s1, s2 Reg), countMask int32) {
	countElem := f.popValue()
	count := f.materialize(countElem)
	f.a.AluRI(4, count, countMask, false) // Wasm shifts use count modulo lane width.

	value := f.popValue()
	x := f.materializeV128(value)
	countX := f.allocFReg(maskOf(x))
	f.a.MovGprToXmm(countX, count, false)
	f.release(count)

	op(x, x, countX)
	f.releaseF(countX)
	f.pushVReg(x)
}

func (f *fn) i16x8Shift(op func(dst, s1, s2 Reg)) { f.v128Shift(op, 15) }

func (f *fn) i32x4Shift(op func(dst, s1, s2 Reg)) { f.v128Shift(op, 31) }

func (f *fn) i64x2Shift(op func(dst, s1, s2 Reg)) { f.v128Shift(op, 63) }

func (f *fn) i8x16NarrowI16x8U() {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	max := f.v128ConstReg(0x00ff00ff00ff00ff, 0x00ff00ff00ff00ff)
	f.a.VPminuw(xa, xa, max)
	f.a.VPminuw(xb, xb, max)
	f.releaseF(max)
	f.fpinned = f.fpinned.remove(xa).remove(xb)

	f.a.VPpackuswb(xa, xa, xb)
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) i16x8NarrowI32x4U() {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	max := f.v128ConstReg(0x0000ffff0000ffff, 0x0000ffff0000ffff)
	f.a.VPminud(xa, xa, max)
	f.a.VPminud(xb, xb, max)
	f.releaseF(max)
	f.fpinned = f.fpinned.remove(xa).remove(xb)

	f.a.VPpackusdw(xa, xa, xb)
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) i16x8ExtendI8x16(signed, high bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	if signed {
		if high {
			f.a.VPunpckhbw(x, x, x)
		} else {
			f.a.VPunpcklbw(x, x, x)
		}
		f.a.VPsrawImm(x, x, 8)
		f.pushVReg(x)
		return
	}

	z := f.allocFReg(maskOf(x))
	f.a.VPxor(z, z, z)
	if high {
		f.a.VPunpckhbw(x, x, z)
	} else {
		f.a.VPunpcklbw(x, x, z)
	}
	f.releaseF(z)
	f.pushVReg(x)
}

func (f *fn) i16x8ExtaddPairwiseI8x16(signed bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	hi := f.allocFReg(maskOf(x))
	f.a.VPor(hi, x, x)
	if signed {
		f.a.VPunpcklbw(x, x, x)
		f.a.VPunpckhbw(hi, hi, hi)
		f.a.VPsrawImm(x, x, 8)
		f.a.VPsrawImm(hi, hi, 8)
	} else {
		z := f.allocFReg(maskOf(x, hi))
		f.a.VPxor(z, z, z)
		f.a.VPunpcklbw(x, x, z)
		f.a.VPunpckhbw(hi, hi, z)
		f.releaseF(z)
	}
	f.a.VPhaddw(x, x, hi)
	f.releaseF(hi)
	f.pushVReg(x)
}

func (f *fn) i16x8ExtmulI8x16(signed, high bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	if signed {
		if high {
			f.a.VPunpckhbw(xa, xa, xa)
			f.a.VPunpckhbw(xb, xb, xb)
		} else {
			f.a.VPunpcklbw(xa, xa, xa)
			f.a.VPunpcklbw(xb, xb, xb)
		}
		f.a.VPsrawImm(xa, xa, 8)
		f.a.VPsrawImm(xb, xb, 8)
	} else {
		z := f.allocFReg(maskOf(xa, xb))
		f.a.VPxor(z, z, z)
		if high {
			f.a.VPunpckhbw(xa, xa, z)
			f.a.VPunpckhbw(xb, xb, z)
		} else {
			f.a.VPunpcklbw(xa, xa, z)
			f.a.VPunpcklbw(xb, xb, z)
		}
		f.releaseF(z)
	}
	f.fpinned = f.fpinned.remove(xa).remove(xb)
	f.a.VPmullw(xa, xa, xb)
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) i32x4ExtendI16x8(signed, high bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	if signed {
		if high {
			f.a.VPunpckhwd(x, x, x)
		} else {
			f.a.VPunpcklwd(x, x, x)
		}
		f.a.VPsradImm(x, x, 16)
		f.pushVReg(x)
		return
	}

	z := f.allocFReg(maskOf(x))
	f.a.VPxor(z, z, z)
	if high {
		f.a.VPunpckhwd(x, x, z)
	} else {
		f.a.VPunpcklwd(x, x, z)
	}
	f.releaseF(z)
	f.pushVReg(x)
}

func (f *fn) i32x4ExtmulI16x8(signed, high bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	if signed {
		if high {
			f.a.VPunpckhwd(xa, xa, xa)
			f.a.VPunpckhwd(xb, xb, xb)
		} else {
			f.a.VPunpcklwd(xa, xa, xa)
			f.a.VPunpcklwd(xb, xb, xb)
		}
		f.a.VPsradImm(xa, xa, 16)
		f.a.VPsradImm(xb, xb, 16)
	} else {
		z := f.allocFReg(maskOf(xa, xb))
		f.a.VPxor(z, z, z)
		if high {
			f.a.VPunpckhwd(xa, xa, z)
			f.a.VPunpckhwd(xb, xb, z)
		} else {
			f.a.VPunpcklwd(xa, xa, z)
			f.a.VPunpcklwd(xb, xb, z)
		}
		f.releaseF(z)
	}
	f.fpinned = f.fpinned.remove(xa).remove(xb)
	f.a.VPmulld(xa, xa, xb)
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) i32x4ExtaddPairwiseI16x8(signed bool) {
	v := f.popValue()
	x := f.materializeV128(v)
	hi := f.allocFReg(maskOf(x))
	f.a.VPor(hi, x, x)
	if signed {
		f.a.VPunpcklwd(x, x, x)
		f.a.VPunpckhwd(hi, hi, hi)
		f.a.VPsradImm(x, x, 16)
		f.a.VPsradImm(hi, hi, 16)
	} else {
		z := f.allocFReg(maskOf(x, hi))
		f.a.VPxor(z, z, z)
		f.a.VPunpcklwd(x, x, z)
		f.a.VPunpckhwd(hi, hi, z)
		f.releaseF(z)
	}
	f.a.VPhaddd(x, x, hi)
	f.releaseF(hi)
	f.pushVReg(x)
}

func (f *fn) i64x2ExtendI32x4(signed, high bool) {
	v := f.popValue()
	x := f.materializeV128(v)

	z := f.allocFReg(maskOf(x))
	f.a.VPxor(z, z, z)
	if signed {
		sign := f.allocFReg(maskOf(x, z))
		f.a.VPcmpgtd(sign, z, x) // sign dword = -1 when lane < 0, else 0.
		if high {
			f.a.VPunpckhdq(x, x, sign)
		} else {
			f.a.VPunpckldq(x, x, sign)
		}
		f.releaseF(sign)
	} else if high {
		f.a.VPunpckhdq(x, x, z)
	} else {
		f.a.VPunpckldq(x, x, z)
	}
	f.releaseF(z)
	f.pushVReg(x)
}

func (f *fn) i64x2ExtmulI32x4(signed, high bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	shuffle := byte(0x10) // lanes 0,1 -> dword positions 0,2 for PMULDQ/PMULUDQ.
	if high {
		shuffle = 0x32 // lanes 2,3 -> dword positions 0,2.
	}
	f.a.Pshufd(xa, xa, shuffle)
	f.a.Pshufd(xb, xb, shuffle)

	f.fpinned = f.fpinned.remove(xa).remove(xb)
	if signed {
		f.a.VPmuldq(xa, xa, xb)
	} else {
		f.a.VPmuludq(xa, xa, xb)
	}
	f.releaseF(xb)
	f.pushVReg(xa)
}

func (f *fn) i16x8Q15mulrSatS() {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	min := f.v128ConstReg(0x8000800080008000, 0x8000800080008000)
	f.fpinned = f.fpinned.add(min)
	mask := f.allocFReg(0)
	f.fpinned = f.fpinned.add(mask)
	f.a.VPcmpeqw(mask, xa, min)
	tmp := f.allocFReg(0)
	f.a.VPcmpeqw(tmp, xb, min)
	f.a.VPand(mask, mask, tmp)
	f.releaseF(tmp)
	f.fpinned = f.fpinned.remove(min)
	f.releaseF(min)

	f.a.VPmulhrsw(xa, xa, xb)
	f.fpinned = f.fpinned.remove(xb)
	f.releaseF(xb)

	max := f.v128ConstReg(0x7fff7fff7fff7fff, 0x7fff7fff7fff7fff)
	f.a.VPand(max, max, mask)
	f.a.VPandn(xa, mask, xa)
	f.a.VPor(xa, xa, max)
	f.releaseF(max)
	f.fpinned = f.fpinned.remove(xa).remove(mask)
	f.releaseF(mask)
	f.pushVReg(xa)
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
	f.a.VPcmpeqb(m, m, m)
	f.a.VPxor(xa, xa, m)
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
		m := f.allocFReg(maskOf(xa))
		f.a.VPcmpeqb(m, m, m)
		f.a.VPxor(xa, xa, m)
		f.releaseF(m)
	}
	f.pushVReg(xa)
}

func (f *fn) v128UnsignedCmp(op func(dst, s1, s2 Reg), signBiasLo, signBiasHi uint64, swap, invert bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)
	bias := f.v128ConstReg(signBiasLo, signBiasHi)
	f.a.VPxor(xa, xa, bias)
	f.a.VPxor(xb, xb, bias)
	f.releaseF(bias)
	f.fpinned = f.fpinned.remove(xa).remove(xb)
	if swap {
		op(xa, xb, xa)
	} else {
		op(xa, xa, xb)
	}
	f.releaseF(xb)
	if invert {
		m := f.allocFReg(maskOf(xa))
		f.a.VPcmpeqb(m, m, m)
		f.a.VPxor(xa, xa, m)
		f.releaseF(m)
	}
	f.pushVReg(xa)
}

const (
	vfcmpEqOQ  = 0x00 // ordered, quiet: false for NaN lanes
	vfcmpNeqUQ = 0x04 // unordered or not-equal, quiet: true for NaN lanes
	vfcmpLtOQ  = 0x11 // ordered, quiet
	vfcmpLeOQ  = 0x12 // ordered, quiet
	vfcmpGeOQ  = 0x1d // ordered, quiet
	vfcmpGtOQ  = 0x1e // ordered, quiet
)

func (f *fn) v128FCmp(f64 bool, pred byte) {
	f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFCmpPacked(dst, s1, s2, f64, pred) })
}

func (f *fn) v128FloatSignOp(f64 bool, op byte, maskLo, maskHi uint64) {
	v := f.popValue()
	x := f.materializeV128(v)
	f.fpinned = f.fpinned.add(x)
	mask := f.v128ConstReg(maskLo, maskHi)
	f.fpinned = f.fpinned.remove(x)
	pp := byte(0)
	if f64 {
		pp = 1
	}
	f.a.VSseRRR(pp, op, x, x, mask)
	f.releaseF(mask)
	f.pushVReg(x)
}

func (f *fn) v128Movemask() Reg {
	v := f.popValue()
	x := f.materializeV128(v)
	r := f.allocReg(0)
	f.a.VPmovmskb(r, x)
	f.releaseF(x)
	return r
}

func (f *fn) v128AnyTrue() {
	v := f.popValue()
	x := f.materializeV128(v)
	z := f.allocFReg(maskOf(x))
	f.a.VPxor(z, z, z)
	f.a.VPcmpeqb(x, x, z) // byte lanes are all-ones only where the original byte was zero.
	f.releaseF(z)
	r := f.allocReg(0)
	f.a.VPmovmskb(r, x)
	f.releaseF(x)
	f.a.AluRI(7, r, 0xffff, false) // cmp r, 0xffff: every byte was zero.
	f.a.SetccReg(condNE, r)
	f.pushReg(r, mtI32)
}

func (f *fn) v128AllTrue(cmpEqZero func(dst, s1, s2 Reg)) {
	v := f.popValue()
	x := f.materializeV128(v)
	z := f.allocFReg(maskOf(x))
	f.a.VPxor(z, z, z)
	cmpEqZero(x, x, z) // lanes are all-ones only where the original lane was zero.
	f.releaseF(z)
	r := f.allocReg(0)
	f.a.VPmovmskb(r, x)
	f.releaseF(x)
	f.a.TestSelf(r, false)
	f.a.SetccReg(condE, r)
	f.pushReg(r, mtI32)
}

func (f *fn) i8x16AllTrue() { f.v128AllTrue(f.a.VPcmpeqb) }

func (f *fn) i16x8AllTrue() { f.v128AllTrue(f.a.VPcmpeqw) }

func (f *fn) i32x4AllTrue() { f.v128AllTrue(f.a.VPcmpeqd) }

func (f *fn) i64x2AllTrue() { f.v128AllTrue(f.a.VPcmpeqq) }

func (f *fn) i8x16Bitmask() {
	r := f.v128Movemask()
	f.pushReg(r, mtI32)
}

func (f *fn) i16x8Bitmask() {
	r := f.v128Movemask()
	t := f.allocReg(maskOf(r))
	f.a.ShiftImm(5, r, 1, false)
	f.a.AluRI(4, r, 0x5555, false)
	f.a.MovRegReg32(t, r)
	f.a.ShiftImm(5, t, 1, false)
	f.a.Or32(r, t)
	f.a.AluRI(4, r, 0x3333, false)
	f.a.MovRegReg32(t, r)
	f.a.ShiftImm(5, t, 2, false)
	f.a.Or32(r, t)
	f.a.AluRI(4, r, 0x0f0f, false)
	f.a.MovRegReg32(t, r)
	f.a.ShiftImm(5, t, 4, false)
	f.a.Or32(r, t)
	f.a.AluRI(4, r, 0x00ff, false)
	f.release(t)
	f.pushReg(r, mtI32)
}

func (f *fn) i32x4Bitmask() {
	r := f.v128Movemask()
	t := f.allocReg(maskOf(r))
	f.a.ShiftImm(5, r, 3, false)
	f.a.AluRI(4, r, 0x1111, false)
	f.a.MovRegReg32(t, r)
	f.a.ShiftImm(5, t, 3, false)
	f.a.Or32(r, t)
	f.a.AluRI(4, r, 0x0303, false)
	f.a.MovRegReg32(t, r)
	f.a.ShiftImm(5, t, 6, false)
	f.a.Or32(r, t)
	f.a.AluRI(4, r, 0x000f, false)
	f.release(t)
	f.pushReg(r, mtI32)
}

func (f *fn) i64x2Bitmask() {
	r := f.v128Movemask()
	t := f.allocReg(maskOf(r))
	f.a.ShiftImm(5, r, 7, false)
	f.a.AluRI(4, r, 0x0101, false)
	f.a.MovRegReg32(t, r)
	f.a.ShiftImm(5, t, 7, false)
	f.a.Or32(r, t)
	f.a.AluRI(4, r, 0x0003, false)
	f.release(t)
	f.pushReg(r, mtI32)
}

func (f *fn) v128Splat(kind uint32) {
	s := f.popValue()
	switch kind {
	case 15: // i8x16.splat
		r := f.materialize(s)
		f.a.AluRI(4, r, 0xff, false) // keep only the low i8 lane, zeroing the high half.
		pat := f.allocReg(maskOf(r))
		f.a.MovImm64(pat, 0x0101010101010101)
		f.a.IMul(r, pat, true)
		f.release(pat)
		x := f.allocFReg(0)
		f.a.MovGprToXmm(x, r, true)
		f.a.Punpcklqdq(x, x)
		f.release(r)
		f.pushVReg(x)
	case 16: // i16x8.splat
		r := f.materialize(s)
		f.a.AluRI(4, r, 0xffff, false)
		pat := f.allocReg(maskOf(r))
		f.a.MovImm64(pat, 0x0001000100010001)
		f.a.IMul(r, pat, true)
		f.release(pat)
		x := f.allocFReg(0)
		f.a.MovGprToXmm(x, r, true)
		f.a.Punpcklqdq(x, x)
		f.release(r)
		f.pushVReg(x)
	case 17: // i32x4.splat
		r := f.materialize(s)
		x := f.allocFReg(0)
		f.a.MovGprToXmm(x, r, false)
		f.a.Pshufd(x, x, 0x00)
		f.release(r)
		f.pushVReg(x)
	case 18: // i64x2.splat
		r := f.materialize(s)
		x := f.allocFReg(0)
		f.a.MovGprToXmm(x, r, true)
		f.a.Punpcklqdq(x, x)
		f.release(r)
		f.pushVReg(x)
	case 19: // f32x4.splat
		x := f.materializeF(s)
		f.a.Pshufd(x, x, 0x00)
		f.pushVReg(x)
	case 20: // f64x2.splat
		x := f.materializeF(s)
		f.a.Punpcklqdq(x, x)
		f.pushVReg(x)
	}
}

func (f *fn) v128ExtractLane(kind uint32, lane byte) {
	v := f.popValue()
	x := f.materializeV128(v)
	switch kind {
	case 21, 22: // i8x16.extract_lane_s/u
		r := f.allocReg(0)
		f.a.Pextrb(r, x, lane)
		if kind == 21 {
			f.a.Movsx8(r, r, false)
		}
		f.releaseF(x)
		f.pushReg(r, mtI32)
	case 24, 25: // i16x8.extract_lane_s/u
		r := f.allocReg(0)
		f.a.Pextrw(r, x, lane)
		if kind == 24 {
			f.a.Movsx16(r, r, false)
		}
		f.releaseF(x)
		f.pushReg(r, mtI32)
	case 27: // i32x4.extract_lane
		r := f.allocReg(0)
		f.a.Pextrd(r, x, lane)
		f.releaseF(x)
		f.pushReg(r, mtI32)
	case 29: // i64x2.extract_lane
		r := f.allocReg(0)
		f.a.Pextrq(r, x, lane)
		f.releaseF(x)
		f.pushReg(r, mtI64)
	case 31: // f32x4.extract_lane
		if lane != 0 {
			f.a.Pshufd(x, x, lane)
		}
		f.pushFReg(x, mtF32)
	case 33: // f64x2.extract_lane
		if lane != 0 {
			f.a.Pshufd(x, x, 0xee)
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
		f.a.Pinsrb(x, r, lane)
		f.release(r)
	case 26: // i16x8.replace_lane
		r := f.materialize(s)
		f.a.Pinsrw(x, r, lane)
		f.release(r)
	case 28: // i32x4.replace_lane
		r := f.materialize(s)
		f.a.Pinsrd(x, r, lane)
		f.release(r)
	case 30: // i64x2.replace_lane
		r := f.materialize(s)
		f.a.Pinsrq(x, r, lane)
		f.release(r)
	case 32: // f32x4.replace_lane
		f.fpinned = f.fpinned.add(x)
		sx := f.materializeF(s)
		r := f.allocReg(0)
		f.a.MovXmmToGpr(r, sx, false)
		f.releaseF(sx)
		f.fpinned = f.fpinned.remove(x)
		f.a.Pinsrd(x, r, lane)
		f.release(r)
	case 34: // f64x2.replace_lane
		f.fpinned = f.fpinned.add(x)
		sx := f.materializeF(s)
		r := f.allocReg(0)
		f.a.MovXmmToGpr(r, sx, true)
		f.releaseF(sx)
		f.fpinned = f.fpinned.remove(x)
		f.a.Pinsrq(x, r, lane)
		f.release(r)
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
	f.a.VMovdquLoadIdx(x, RBX, ea, disp)
	if eaOwned {
		f.release(ea)
	}
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
	f.materializePendingLoads()
	v := f.popValue()
	x := f.materializeV128(v)
	f.fpinned = f.fpinned.add(x)
	ea, eaOwned, _, disp := f.memAddr(off, 16, true)
	f.a.VMovdquStoreIdx(RBX, ea, x, disp)
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
	case 11: // v128.store
		return f.v128Store(r)
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
		f.v128Bin(f.a.VPcmpeqb)
	case 36: // i8x16.ne
		f.v128BinNot(f.a.VPcmpeqb)
	case 37: // i8x16.lt_s
		f.v128SignedCmp(f.a.VPcmpgtb, true, false)
	case 38: // i8x16.lt_u
		f.v128UnsignedCmp(f.a.VPcmpgtb, 0x8080808080808080, 0x8080808080808080, true, false)
	case 39: // i8x16.gt_s
		f.v128Bin(f.a.VPcmpgtb)
	case 40: // i8x16.gt_u
		f.v128UnsignedCmp(f.a.VPcmpgtb, 0x8080808080808080, 0x8080808080808080, false, false)
	case 41: // i8x16.le_s
		f.v128SignedCmp(f.a.VPcmpgtb, false, true)
	case 42: // i8x16.le_u
		f.v128UnsignedCmp(f.a.VPcmpgtb, 0x8080808080808080, 0x8080808080808080, false, true)
	case 43: // i8x16.ge_s
		f.v128SignedCmp(f.a.VPcmpgtb, true, true)
	case 44: // i8x16.ge_u
		f.v128UnsignedCmp(f.a.VPcmpgtb, 0x8080808080808080, 0x8080808080808080, true, true)
	case 45: // i16x8.eq
		f.v128Bin(f.a.VPcmpeqw)
	case 46: // i16x8.ne
		f.v128BinNot(f.a.VPcmpeqw)
	case 47: // i16x8.lt_s
		f.v128SignedCmp(f.a.VPcmpgtw, true, false)
	case 48: // i16x8.lt_u
		f.v128UnsignedCmp(f.a.VPcmpgtw, 0x8000800080008000, 0x8000800080008000, true, false)
	case 49: // i16x8.gt_s
		f.v128Bin(f.a.VPcmpgtw)
	case 50: // i16x8.gt_u
		f.v128UnsignedCmp(f.a.VPcmpgtw, 0x8000800080008000, 0x8000800080008000, false, false)
	case 51: // i16x8.le_s
		f.v128SignedCmp(f.a.VPcmpgtw, false, true)
	case 52: // i16x8.le_u
		f.v128UnsignedCmp(f.a.VPcmpgtw, 0x8000800080008000, 0x8000800080008000, false, true)
	case 53: // i16x8.ge_s
		f.v128SignedCmp(f.a.VPcmpgtw, true, true)
	case 54: // i16x8.ge_u
		f.v128UnsignedCmp(f.a.VPcmpgtw, 0x8000800080008000, 0x8000800080008000, true, true)
	case 55: // i32x4.eq
		f.v128Bin(f.a.VPcmpeqd)
	case 56: // i32x4.ne
		f.v128BinNot(f.a.VPcmpeqd)
	case 57: // i32x4.lt_s
		f.v128SignedCmp(f.a.VPcmpgtd, true, false)
	case 58: // i32x4.lt_u
		f.v128UnsignedCmp(f.a.VPcmpgtd, 0x8000000080000000, 0x8000000080000000, true, false)
	case 59: // i32x4.gt_s
		f.v128Bin(f.a.VPcmpgtd)
	case 60: // i32x4.gt_u
		f.v128UnsignedCmp(f.a.VPcmpgtd, 0x8000000080000000, 0x8000000080000000, false, false)
	case 61: // i32x4.le_s
		f.v128SignedCmp(f.a.VPcmpgtd, false, true)
	case 62: // i32x4.le_u
		f.v128UnsignedCmp(f.a.VPcmpgtd, 0x8000000080000000, 0x8000000080000000, false, true)
	case 63: // i32x4.ge_s
		f.v128SignedCmp(f.a.VPcmpgtd, true, true)
	case 64: // i32x4.ge_u
		f.v128UnsignedCmp(f.a.VPcmpgtd, 0x8000000080000000, 0x8000000080000000, true, true)
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
		f.v128Bin(f.a.VPpacksswb)
	case 102: // i8x16.narrow_i16x8_u
		f.i8x16NarrowI16x8U()
	case 110: // i8x16.add
		f.v128Bin(f.a.VPaddb)
	case 111: // i8x16.add_sat_s
		f.v128Bin(f.a.VPaddsb)
	case 112: // i8x16.add_sat_u
		f.v128Bin(f.a.VPaddusb)
	case 113: // i8x16.sub
		f.v128Bin(f.a.VPsubb)
	case 114: // i8x16.sub_sat_s
		f.v128Bin(f.a.VPsubsb)
	case 115: // i8x16.sub_sat_u
		f.v128Bin(f.a.VPsubusb)
	case 118: // i8x16.min_s
		f.v128Bin(f.a.VPminsb)
	case 119: // i8x16.min_u
		f.v128Bin(f.a.VPminub)
	case 120: // i8x16.max_s
		f.v128Bin(f.a.VPmaxsb)
	case 121: // i8x16.max_u
		f.v128Bin(f.a.VPmaxub)
	case 123: // i8x16.avgr_u
		f.v128Bin(f.a.VPavgb)
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
		f.v128Bin(f.a.VPpackssdw)
	case 134: // i16x8.narrow_i32x4_u
		f.i16x8NarrowI32x4U()
	case 135: // i16x8.extend_low_i8x16_s
		f.i16x8ExtendI8x16(true, false)
	case 136: // i16x8.extend_high_i8x16_s
		f.i16x8ExtendI8x16(true, true)
	case 137: // i16x8.extend_low_i8x16_u
		f.i16x8ExtendI8x16(false, false)
	case 138: // i16x8.extend_high_i8x16_u
		f.i16x8ExtendI8x16(false, true)
	case 139: // i16x8.shl
		f.i16x8Shift(f.a.VPsllw)
	case 140: // i16x8.shr_s
		f.i16x8Shift(f.a.VPsraw)
	case 141: // i16x8.shr_u
		f.i16x8Shift(f.a.VPsrlw)
	case 142: // i16x8.add
		f.v128Bin(f.a.VPaddw)
	case 143: // i16x8.add_sat_s
		f.v128Bin(f.a.VPaddsw)
	case 144: // i16x8.add_sat_u
		f.v128Bin(f.a.VPaddusw)
	case 145: // i16x8.sub
		f.v128Bin(f.a.VPsubw)
	case 146: // i16x8.sub_sat_s
		f.v128Bin(f.a.VPsubsw)
	case 147: // i16x8.sub_sat_u
		f.v128Bin(f.a.VPsubusw)
	case 149: // i16x8.mul
		f.v128Bin(f.a.VPmullw)
	case 150: // i16x8.min_s
		f.v128Bin(f.a.VPminsw)
	case 151: // i16x8.min_u
		f.v128Bin(f.a.VPminuw)
	case 152: // i16x8.max_s
		f.v128Bin(f.a.VPmaxsw)
	case 153: // i16x8.max_u
		f.v128Bin(f.a.VPmaxuw)
	case 155: // i16x8.avgr_u
		f.v128Bin(f.a.VPavgw)
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
		f.i32x4Shift(f.a.VPslld)
	case 172: // i32x4.shr_s
		f.i32x4Shift(f.a.VPsrad)
	case 173: // i32x4.shr_u
		f.i32x4Shift(f.a.VPsrld)
	case 199: // i64x2.extend_low_i32x4_s
		f.i64x2ExtendI32x4(true, false)
	case 200: // i64x2.extend_high_i32x4_s
		f.i64x2ExtendI32x4(true, true)
	case 201: // i64x2.extend_low_i32x4_u
		f.i64x2ExtendI32x4(false, false)
	case 202: // i64x2.extend_high_i32x4_u
		f.i64x2ExtendI32x4(false, true)
	case 203: // i64x2.shl
		f.i64x2Shift(f.a.VPsllq)
	case 205: // i64x2.shr_u
		f.i64x2Shift(f.a.VPsrlq)
	case 174: // i32x4.add
		f.v128Bin(f.a.VPaddd)
	case 177: // i32x4.sub
		f.v128Bin(f.a.VPsubd)
	case 181: // i32x4.mul
		f.v128Bin(f.a.VPmulld)
	case 182: // i32x4.min_s
		f.v128Bin(f.a.VPminsd)
	case 183: // i32x4.min_u
		f.v128Bin(f.a.VPminud)
	case 184: // i32x4.max_s
		f.v128Bin(f.a.VPmaxsd)
	case 185: // i32x4.max_u
		f.v128Bin(f.a.VPmaxud)
	case 188: // i32x4.extmul_low_i16x8_s
		f.i32x4ExtmulI16x8(true, false)
	case 189: // i32x4.extmul_high_i16x8_s
		f.i32x4ExtmulI16x8(true, true)
	case 190: // i32x4.extmul_low_i16x8_u
		f.i32x4ExtmulI16x8(false, false)
	case 191: // i32x4.extmul_high_i16x8_u
		f.i32x4ExtmulI16x8(false, true)
	case 206: // i64x2.add
		f.v128Bin(f.a.VPaddq)
	case 209: // i64x2.sub
		f.v128Bin(f.a.VPsubq)
	case 220: // i64x2.extmul_low_i32x4_s
		f.i64x2ExtmulI32x4(true, false)
	case 221: // i64x2.extmul_high_i32x4_s
		f.i64x2ExtmulI32x4(true, true)
	case 222: // i64x2.extmul_low_i32x4_u
		f.i64x2ExtmulI32x4(false, false)
	case 223: // i64x2.extmul_high_i32x4_u
		f.i64x2ExtmulI32x4(false, true)
	case 214: // i64x2.eq
		f.v128Bin(f.a.VPcmpeqq)
	case 215: // i64x2.ne
		f.v128BinNot(f.a.VPcmpeqq)
	case 224: // f32x4.abs
		f.v128FloatSignOp(false, 0x54, 0x7fffffff7fffffff, 0x7fffffff7fffffff)
	case 225: // f32x4.neg
		f.v128FloatSignOp(false, 0x57, 0x8000000080000000, 0x8000000080000000)
	case 227: // f32x4.sqrt
		f.v128IntegerAbs(func(dst, src Reg) { f.a.VFPackedSqrt(dst, src, false) })
	case 228: // f32x4.add
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFPackedAdd(dst, s1, s2, false) })
	case 229: // f32x4.sub
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFPackedSub(dst, s1, s2, false) })
	case 230: // f32x4.mul
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFPackedMul(dst, s1, s2, false) })
	case 231: // f32x4.div
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFPackedDiv(dst, s1, s2, false) })
	case 236: // f64x2.abs
		f.v128FloatSignOp(true, 0x54, 0x7fffffffffffffff, 0x7fffffffffffffff)
	case 237: // f64x2.neg
		f.v128FloatSignOp(true, 0x57, 0x8000000000000000, 0x8000000000000000)
	case 239: // f64x2.sqrt
		f.v128IntegerAbs(func(dst, src Reg) { f.a.VFPackedSqrt(dst, src, true) })
	case 240: // f64x2.add
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFPackedAdd(dst, s1, s2, true) })
	case 241: // f64x2.sub
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFPackedSub(dst, s1, s2, true) })
	case 242: // f64x2.mul
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFPackedMul(dst, s1, s2, true) })
	case 243: // f64x2.div
		f.v128Bin(func(dst, s1, s2 Reg) { f.a.VFPackedDiv(dst, s1, s2, true) })
	case 83: // v128.any_true
		f.v128AnyTrue()
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
		f.v128IntegerAbs(f.a.VPabsb)
	case 97: // i8x16.neg
		f.v128IntegerNeg(f.a.VPsubb)
	case 98: // i8x16.popcnt
		f.i8x16Popcnt()
	case 128: // i16x8.abs
		f.v128IntegerAbs(f.a.VPabsw)
	case 129: // i16x8.neg
		f.v128IntegerNeg(f.a.VPsubw)
	case 160: // i32x4.abs
		f.v128IntegerAbs(f.a.VPabsd)
	case 161: // i32x4.neg
		f.v128IntegerNeg(f.a.VPsubd)
	case 193: // i64x2.neg
		f.v128IntegerNeg(f.a.VPsubq)
	case 77: // v128.not
		f.v128UnaryNot()
	case 78: // v128.and
		f.v128Bin(f.a.VPand)
	case 79: // v128.andnot (a &^ b)
		// VPANDN computes ^s1 & s2, so swap via explicit not+and for Wasm a & ~b.
		b := f.popValue()
		a := f.popValue()
		xa := f.materializeV128(a)
		f.fpinned = f.fpinned.add(xa)
		xb := f.materializeV128(b)
		m := f.allocFReg(maskOf(xa, xb))
		f.a.VPcmpeqb(m, m, m)
		f.a.VPxor(xb, xb, m)
		f.releaseF(m)
		f.fpinned = f.fpinned.remove(xa)
		f.a.VPand(xa, xa, xb)
		f.releaseF(xb)
		f.pushVReg(xa)
	case 80: // v128.or
		f.v128Bin(f.a.VPor)
	case 81: // v128.xor
		f.v128Bin(f.a.VPxor)
	case 82: // v128.bitselect: (a & mask) | (b & ~mask)
		maskElem := f.popValue()
		bElem := f.popValue()
		aElem := f.popValue()
		mask := f.materializeV128(maskElem)
		f.fpinned = f.fpinned.add(mask)
		xb := f.materializeV128(bElem)
		f.fpinned = f.fpinned.add(xb)
		xa := f.materializeV128(aElem)
		f.a.VPand(xa, xa, mask)
		f.a.VPandn(xb, mask, xb)
		f.a.VPor(xa, xa, xb)
		f.fpinned = f.fpinned.remove(mask).remove(xb)
		f.releaseF(mask)
		f.releaseF(xb)
		f.pushVReg(xa)
	default:
		return fmt.Errorf("amd64: unsupported 0xFD opcode %d", sub)
	}
	return nil
}
