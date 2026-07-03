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
	default:
		return fmt.Errorf("amd64: unsupported 0xFD opcode %d", sub)
	}
	return nil
}
