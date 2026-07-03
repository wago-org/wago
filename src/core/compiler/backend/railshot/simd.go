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

func (f *fn) v128Const(lo, hi uint64) {
	x := f.allocFReg(0)
	if lo == 0 && hi == 0 {
		f.a.VPxor(x, x, x)
		f.pushVReg(x)
		return
	}
	slot := f.allocSpillSlots(2)
	t := f.allocReg(0)
	f.a.MovImm64(t, lo)
	f.a.Store64(RSP, f.spillOff(slot), t)
	f.a.MovImm64(t, hi)
	f.a.Store64(RSP, f.spillOff(slot)+8, t)
	f.release(t)
	f.a.VMovdquLoadDisp(x, RSP, f.spillOff(slot))
	f.pushVReg(x)
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
	case 39: // i8x16.gt_s
		f.v128Bin(f.a.VPcmpgtb)
	case 45: // i16x8.eq
		f.v128Bin(f.a.VPcmpeqw)
	case 46: // i16x8.ne
		f.v128BinNot(f.a.VPcmpeqw)
	case 49: // i16x8.gt_s
		f.v128Bin(f.a.VPcmpgtw)
	case 55: // i32x4.eq
		f.v128Bin(f.a.VPcmpeqd)
	case 56: // i32x4.ne
		f.v128BinNot(f.a.VPcmpeqd)
	case 59: // i32x4.gt_s
		f.v128Bin(f.a.VPcmpgtd)
	case 110: // i8x16.add
		f.v128Bin(f.a.VPaddb)
	case 113: // i8x16.sub
		f.v128Bin(f.a.VPsubb)
	case 142: // i16x8.add
		f.v128Bin(f.a.VPaddw)
	case 145: // i16x8.sub
		f.v128Bin(f.a.VPsubw)
	case 174: // i32x4.add
		f.v128Bin(f.a.VPaddd)
	case 177: // i32x4.sub
		f.v128Bin(f.a.VPsubd)
	case 206: // i64x2.add
		f.v128Bin(f.a.VPaddq)
	case 209: // i64x2.sub
		f.v128Bin(f.a.VPsubq)
	case 214: // i64x2.eq
		f.v128Bin(f.a.VPcmpeqq)
	case 215: // i64x2.ne
		f.v128BinNot(f.a.VPcmpeqq)
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
