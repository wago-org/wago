package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func wideDstForResult(below, prior []machineType) int {
	slot := 0
	for _, t := range below {
		slot += t.stackSlots()
	}
	for _, t := range prior {
		slot += t.stackSlots()
	}
	return slot
}

// v256 values are canonical frame residents between operations and materialize
// into one YMM register for AVX2 lowering.
func (f *fn) loadV256(e *elem) Reg {
	if e.st.kind == stReg {
		return e.st.reg
	}
	x := f.allocFReg(0)
	switch e.st.kind {
	case stConst:
		if e.st.cval != 0 {
			panic("amd64: nonzero immediate v256 storage")
		}
		f.a.YPxor(x, x, x)
	case stSlot:
		f.a.YMovdquLoadDisp(x, RSP, f.spillOff(e.st.slot))
	case stLocalRef, stLocalReg:
		f.a.YMovdquLoadDisp(x, RSP, f.localOff(e.st.idx))
	default:
		panic("amd64: non-canonical v256 storage")
	}
	f.occupyF(e, x)
	return x
}

func (f *fn) pushYReg(r Reg) *elem {
	e := f.pushValue(storage{kind: stReg, typ: mtV256, reg: r})
	f.fregUser[r] = e
	return e
}

func (f *fn) storeV256(e *elem, slot int) {
	x := f.loadV256(e)
	f.a.YMovdquStoreDisp(RSP, f.spillOff(slot), x)
	f.releaseF(x)
}

func (f *fn) storeV256Local(e *elem, local int) {
	x := f.loadV256(e)
	f.a.YMovdquStoreDisp(RSP, f.localOff(local), x)
	f.releaseF(x)
}

func (f *fn) copySpillV256(src, dst int) {
	x := f.allocFReg(0)
	f.a.YMovdquLoadDisp(x, RSP, f.spillOff(src))
	f.a.YMovdquStoreDisp(RSP, f.spillOff(dst), x)
	f.releaseF(x)
}

func (f *fn) v256Const(r *wasm.Reader) error {
	var b [32]byte
	for i := range b {
		v, err := r.Byte()
		if err != nil {
			return err
		}
		b[i] = v
	}
	lo := f.v128ConstReg(u64le(b[0:8]), u64le(b[8:16]))
	f.fpinned = f.fpinned.add(lo)
	hi := f.v128ConstReg(u64le(b[16:24]), u64le(b[24:32]))
	f.a.YInsertI128(lo, lo, hi, 1)
	f.fpinned = f.fpinned.remove(lo)
	f.releaseF(hi)
	f.pushYReg(lo)
	return nil
}

func u64le(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

func (f *fn) v256Bin(op func(dst, s1, s2 Reg)) {
	b, a := f.popValue(), f.popValue()
	xa := f.loadV256(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.loadV256(b)
	f.fpinned = f.fpinned.remove(xa)
	op(xa, xa, xb)
	f.releaseF(xb)
	f.pushYReg(xa)
}

func (f *fn) v256Unary(op func(dst, src Reg)) {
	a := f.popValue()
	x := f.loadV256(a)
	op(x, x)
	f.pushYReg(x)
}

func (f *fn) v256IntegerNeg(op func(dst, s1, s2 Reg)) {
	a := f.popValue()
	x := f.loadV256(a)
	z := f.allocFReg(maskOf(x))
	f.a.YPxor(z, z, z)
	op(x, z, x)
	f.releaseF(z)
	f.pushYReg(x)
}

func (f *fn) v256Repeated128Const(lo, hi uint64) Reg {
	x := f.v128ConstReg(lo, hi)
	f.a.YInsertI128(x, x, x, 1)
	return x
}

func (f *fn) v256BinNot(op func(dst, s1, s2 Reg)) {
	f.v256Bin(func(dst, a, b Reg) {
		op(dst, a, b)
		m := f.allocFReg(maskOf(dst))
		f.a.YPcmpeqb(m, m, m)
		f.a.YPxor(dst, dst, m)
		f.releaseF(m)
	})
}

func (f *fn) v256SignedCmp(op func(dst, s1, s2 Reg), swap, invert bool) {
	f.v256Bin(func(dst, a, b Reg) {
		if swap {
			a, b = b, a
		}
		op(dst, a, b)
		if invert {
			m := f.allocFReg(maskOf(dst))
			f.a.YPcmpeqb(m, m, m)
			f.a.YPxor(dst, dst, m)
			f.releaseF(m)
		}
	})
}

func (f *fn) v256UnsignedCmp(op func(dst, s1, s2 Reg), biasLo, biasHi uint64, swap, invert bool) {
	f.v256Bin(func(dst, a, b Reg) {
		bias := f.v256Repeated128Const(biasLo, biasHi)
		f.a.YPxor(a, a, bias)
		f.a.YPxor(b, b, bias)
		f.releaseF(bias)
		if swap {
			a, b = b, a
		}
		op(dst, a, b)
		if invert {
			m := f.allocFReg(maskOf(dst))
			f.a.YPcmpeqb(m, m, m)
			f.a.YPxor(dst, dst, m)
			f.releaseF(m)
		}
	})
}

func (f *fn) v256FCmp(f64 bool, pred byte) {
	f.v256Bin(func(dst, a, b Reg) { f.a.YFCmpPacked(dst, a, b, f64, pred) })
}

func (f *fn) v256FloatSignOp(f64 bool, op byte, lo, hi uint64) {
	a := f.popValue()
	x := f.loadV256(a)
	mask := f.v256Repeated128Const(lo, hi)
	pp := byte(0)
	if f64 {
		pp = 1
	}
	f.a.YSseRRR(pp, op, x, x, mask)
	f.releaseF(mask)
	f.pushYReg(x)
}

func (f *fn) v256FloatMinMax(f64, isMax bool) {
	bElem, aElem := f.popValue(), f.popValue()
	a := f.loadV256(aElem)
	f.fpinned = f.fpinned.add(a)
	b := f.loadV256(bElem)
	f.fpinned = f.fpinned.add(b)
	t := f.allocFReg(maskOf(a, b))
	f.fpinned = f.fpinned.add(t)
	c := f.allocFReg(maskOf(a, b, t))
	packed := f.a.YFPackedMin
	if isMax {
		packed = f.a.YFPackedMax
	}
	packed(t, a, b, f64)
	packed(a, b, a, f64)
	f.a.YFCmpPacked(c, t, a, f64, 0x03)
	pp := byte(0)
	if f64 {
		pp = 1
	}
	if isMax {
		f.a.YSseRRR(pp, 0x54, t, t, a)
	} else {
		f.a.YSseRRR(pp, 0x56, t, t, a)
	}
	f.a.YSseRRR(pp, 0x56, t, t, c)
	if f64 {
		f.a.YPsrlqImm(c, c, 13)
	} else {
		f.a.YPsrldImm(c, c, 10)
	}
	f.a.YSseRRR(pp, 0x55, t, c, t)
	f.fpinned = f.fpinned.remove(t).remove(a).remove(b)
	f.releaseF(c)
	f.releaseF(a)
	f.releaseF(b)
	f.pushYReg(t)
}

func (f *fn) v256AllTrue(cmp func(dst, s1, s2 Reg)) {
	a := f.popValue()
	x := f.loadV256(a)
	z := f.allocFReg(maskOf(x))
	f.a.YPxor(z, z, z)
	cmp(x, x, z)
	f.releaseF(z)
	r := f.allocReg(0)
	f.a.YPmovmskb(r, x)
	f.releaseF(x)
	f.a.TestSelf(r, false)
	f.a.SetccReg(condE, r)
	f.pushReg(r, mtI32)
}

func (f *fn) v256I8Bitmask() {
	a := f.popValue()
	x := f.loadV256(a)
	r := f.allocReg(0)
	f.a.YPmovmskb(r, x)
	f.releaseF(x)
	f.pushReg(r, mtI32)
}

func (f *fn) v256Shift(op func(dst, s1, s2 Reg), countMask int32) {
	countElem := f.popValue()
	count := f.materialize(countElem)
	f.a.AluRI(4, count, countMask, false)
	value := f.popValue()
	x := f.loadV256(value)
	countX := f.allocFReg(maskOf(x))
	f.a.MovGprToXmm(countX, count, false)
	f.release(count)
	op(x, x, countX)
	f.releaseF(countX)
	f.pushYReg(x)
}

func (f *fn) v256I8Popcnt() {
	a := f.popValue()
	x := f.loadV256(a)
	f.fpinned = f.fpinned.add(x)
	hi := f.allocFReg(0)
	f.fpinned = f.fpinned.add(hi)
	f.a.YPsrlwImm(hi, x, 4)
	mask := f.v256Repeated128Const(0x0f0f0f0f0f0f0f0f, 0x0f0f0f0f0f0f0f0f)
	f.fpinned = f.fpinned.add(mask)
	lut := f.v256Repeated128Const(0x0302020102010100, 0x0403030203020201)
	f.a.YPand(x, x, mask)
	f.a.YPand(hi, hi, mask)
	f.fpinned = f.fpinned.remove(mask)
	f.releaseF(mask)
	f.a.YPshufb(x, lut, x)
	f.a.YPshufb(hi, lut, hi)
	f.releaseF(lut)
	f.a.YPaddb(x, x, hi)
	f.fpinned = f.fpinned.remove(x).remove(hi)
	f.releaseF(hi)
	f.pushYReg(x)
}

func (f *fn) v256RelaxedMadd(f64, neg bool) {
	cElem, bElem, aElem := f.popValue(), f.popValue(), f.popValue()
	a := f.loadV256(aElem)
	f.fpinned = f.fpinned.add(a)
	b := f.loadV256(bElem)
	f.fpinned = f.fpinned.add(b)
	c := f.loadV256(cElem)
	f.a.YFPackedMul(a, a, b, f64)
	f.fpinned = f.fpinned.remove(b)
	f.releaseF(b)
	if neg {
		f.a.YFPackedSub(c, c, a, f64)
		f.fpinned = f.fpinned.remove(a)
		f.releaseF(a)
		f.pushYReg(c)
		return
	}
	f.a.YFPackedAdd(a, a, c, f64)
	f.releaseF(c)
	f.fpinned = f.fpinned.remove(a)
	f.pushYReg(a)
}

func (f *fn) v256Not() {
	a := f.popValue()
	x := f.loadV256(a)
	m := f.allocFReg(maskOf(x))
	f.a.YPcmpeqb(m, m, m)
	f.a.YPxor(x, x, m)
	f.releaseF(m)
	f.pushYReg(x)
}

func (f *fn) v256Andnot() {
	f.v256Bin(func(dst, a, b Reg) {
		m := f.allocFReg(maskOf(a, b))
		f.a.YPcmpeqb(m, m, m)
		f.a.YPxor(b, b, m)
		f.releaseF(m)
		f.a.YPand(dst, a, b)
	})
}

func (f *fn) v256Bitselect() {
	maskElem, bElem, aElem := f.popValue(), f.popValue(), f.popValue()
	mask := f.loadV256(maskElem)
	f.fpinned = f.fpinned.add(mask)
	a := f.loadV256(aElem)
	f.fpinned = f.fpinned.add(a)
	b := f.loadV256(bElem)
	f.a.YPand(a, a, mask)
	f.a.YPandn(b, mask, b)
	f.a.YPor(a, a, b)
	f.fpinned = f.fpinned.remove(mask).remove(a)
	f.releaseF(mask)
	f.releaseF(b)
	f.pushYReg(a)
}

func (f *fn) v256AnyTrue() {
	a := f.popValue()
	x := f.loadV256(a)
	z := f.allocFReg(maskOf(x))
	f.a.YPxor(z, z, z)
	f.a.YPcmpeqb(x, x, z)
	f.releaseF(z)
	r := f.allocReg(0)
	f.a.YPmovmskb(r, x)
	f.releaseF(x)
	f.a.AluRI(7, r, -1, false)
	f.a.SetccReg(condNE, r)
	f.pushReg(r, mtI32)
}

func (f *fn) v256Select() {
	f.flush()
	cond, b, a := f.popValue(), f.popValue(), f.popValue()
	cr := f.materialize(cond)
	f.pinned = f.pinned.add(cr)
	xa := f.loadV256(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.loadV256(b)
	f.a.TestSelf(cr, false)
	skip := f.a.JccPlaceholder(condNE)
	f.a.YMovdqu(xa, xb)
	f.a.PatchRel32(skip, f.a.Len())
	f.fpinned = f.fpinned.remove(xa)
	f.releaseF(xb)
	f.pinned = f.pinned.remove(cr)
	f.release(cr)
	f.pushYReg(xa)
}

func (f *fn) v256Load(r *wasm.Reader) error {
	if _, err := r.U32(); err != nil {
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	f.flush()
	ea, owned, _, disp := f.memAddr(off, 32, true)
	x := f.allocFReg(0)
	f.a.YMovdquLoadIdx(x, RBX, ea, disp)
	f.pushYReg(x)
	if owned {
		f.release(ea)
	}
	return nil
}

func (f *fn) v256Store(r *wasm.Reader) error {
	if _, err := r.U32(); err != nil {
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	f.flush()
	v := f.popValue()
	ea, owned, _, disp := f.memAddr(off, 32, true)
	x := f.loadV256(v)
	f.a.YMovdquStoreIdx(RBX, ea, x, disp)
	f.releaseF(x)
	if owned {
		f.release(ea)
	}
	return nil
}

func (f *fn) pushV128Slot(slot int) {
	f.pushValue(storage{kind: stSlot, typ: mtV128, slot: slot})
}

func (f *fn) storeTopV128(slot int) {
	e := f.popValue()
	x := f.materializeV128(e)
	f.a.VMovdquStoreDisp(RSP, f.spillOff(slot), x)
	f.releaseF(x)
}

func (f *fn) pushV256Slot(slot int) {
	x := f.allocFReg(0)
	f.a.YMovdquLoadDisp(x, RSP, f.spillOff(slot))
	f.pushYReg(x)
}

func (f *fn) emitV128Half(sub uint32) error {
	return f.emitFDSub(sub, wasm.NewReader(nil))
}

func appendWideULEB(dst []byte, x uint32) []byte {
	for {
		b := byte(x & 0x7f)
		x >>= 7
		if x != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if x == 0 {
			return dst
		}
	}
}

func wideMemReader(align, offset uint32, lane *byte) *wasm.Reader {
	b := appendWideULEB(nil, align)
	b = appendWideULEB(b, offset)
	if lane != nil {
		b = append(b, *lane)
	}
	return wasm.NewReader(b)
}

func (f *fn) v256MirrorLoad(sub uint32, r *wasm.Reader) error {
	align, err := r.U32()
	if err != nil {
		return err
	}
	offset, err := r.U32()
	if err != nil {
		return err
	}
	f.flush()
	out := f.allocSpillSlots(4)
	addr := f.popValue()
	if sub >= 7 && sub <= 10 { // scalar splat: load once and broadcast the XMM half.
		f.pushValue(storage{kind: stSlot, typ: addr.st.typ, slot: addr.st.slot})
		if err := f.emitFDSub(sub, wideMemReader(align, offset, nil)); err != nil {
			return err
		}
		e := f.popValue()
		x := f.materializeV128(e)
		f.a.YInsertI128(x, x, x, 1)
		f.pushYReg(x)
		return nil
	}
	if sub == 92 || sub == 93 { // load_zero fills only the low scalar lanes.
		f.pushValue(storage{kind: stSlot, typ: addr.st.typ, slot: addr.st.slot})
		if err := f.emitFDSub(sub, wideMemReader(align, offset, nil)); err != nil {
			return err
		}
		lo := f.materializeV128(f.popValue())
		y := f.allocFReg(maskOf(lo))
		f.a.YPxor(y, y, y)
		f.a.YInsertI128(y, y, lo, 0)
		f.releaseF(lo)
		f.pushYReg(y)
		return nil
	}
	for half := 0; half < 2; half++ { // widening loads consume eight source bytes per half.
		f.pushValue(storage{kind: stSlot, typ: addr.st.typ, slot: addr.st.slot})
		if err := f.emitFDSub(sub, wideMemReader(align, offset+uint32(half*8), nil)); err != nil {
			return err
		}
		f.storeTopV128(out + half*2)
	}
	f.pushV256Slot(out)
	return nil
}

func v256HalfLanes(sub uint32) int {
	switch sub {
	case 21, 22, 23, 84, 88:
		return 16
	case 24, 25, 26, 85, 89:
		return 8
	case 27, 28, 31, 32, 86, 90:
		return 4
	case 29, 30, 33, 34, 87, 91:
		return 2
	}
	return 0
}

func (f *fn) v256MirrorLane(sub uint32, r *wasm.Reader) error {
	lane, err := r.Byte()
	if err != nil {
		return err
	}
	halfLanes := v256HalfLanes(sub)
	half, localLane := int(lane)/halfLanes, lane%byte(halfLanes)
	f.flush()
	if sub == 23 || sub == 26 || sub == 28 || sub == 30 || sub == 32 || sub == 34 {
		scalar, vector := f.popValue(), f.popValue()
		f.pushV128Slot(vector.st.slot + half*2)
		f.pushValue(storage{kind: stSlot, typ: scalar.st.typ, slot: scalar.st.slot})
		if err := f.emitFDSub(sub, wasm.NewReader([]byte{localLane})); err != nil {
			return err
		}
		f.storeTopV128(vector.st.slot + half*2)
		f.pushV256Slot(vector.st.slot)
		return nil
	}
	vector := f.popValue()
	f.pushV128Slot(vector.st.slot + half*2)
	return f.emitFDSub(sub, wasm.NewReader([]byte{localLane}))
}

func (f *fn) v256MirrorMemLane(sub uint32, r *wasm.Reader) error {
	align, err := r.U32()
	if err != nil {
		return err
	}
	offset, err := r.U32()
	if err != nil {
		return err
	}
	lane, err := r.Byte()
	if err != nil {
		return err
	}
	halfLanes := v256HalfLanes(sub)
	half, localLane := int(lane)/halfLanes, lane%byte(halfLanes)
	f.flush()
	vector, addr := f.popValue(), f.popValue()
	f.pushValue(storage{kind: stSlot, typ: addr.st.typ, slot: addr.st.slot})
	f.pushV128Slot(vector.st.slot + half*2)
	if err := f.emitFDSub(sub, wideMemReader(align, offset, &localLane)); err != nil {
		return err
	}
	if sub >= 84 && sub <= 87 {
		f.storeTopV128(vector.st.slot + half*2)
		f.pushV256Slot(vector.st.slot)
	}
	return nil
}

func (f *fn) v256Shuffle(r *wasm.Reader) error {
	var lanes [32]byte
	for i := range lanes {
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		if lane >= 64 {
			return fmt.Errorf("amd64: invalid i8x32.shuffle lane %d", lane)
		}
		lanes[i] = lane
	}
	f.flush()
	outBase := f.allocSpillSlots(4)
	b, a := f.popValue(), f.popValue()
	sources := [4]int{a.st.slot, a.st.slot + 2, b.st.slot, b.st.slot + 2}
	for outHalf := 0; outHalf < 2; outHalf++ {
		out := f.allocFReg(0)
		f.fpinned = f.fpinned.add(out)
		f.a.VPxor(out, out, out)
		for srcHalf, srcSlot := range sources {
			var maskBytes [16]byte
			for i := range maskBytes {
				maskBytes[i] = 0x80
				lane := lanes[outHalf*16+i]
				if int(lane)/16 == srcHalf {
					maskBytes[i] = lane & 15
				}
			}
			lo, hi := v128MaskBits(maskBytes)
			mask := f.v128ConstReg(lo, hi)
			f.fpinned = f.fpinned.add(mask)
			src := f.allocFReg(maskOf(out, mask))
			f.a.VMovdquLoadDisp(src, RSP, f.spillOff(srcSlot))
			f.a.VPshufb(src, src, mask)
			f.fpinned = f.fpinned.remove(mask)
			f.releaseF(mask)
			f.a.VPor(out, out, src)
			f.releaseF(src)
		}
		f.fpinned = f.fpinned.remove(out)
		f.a.VMovdquStoreDisp(RSP, f.spillOff(outBase+outHalf*2), out)
		f.releaseF(out)
	}
	f.pushV256Slot(outBase)
	return nil
}

func (f *fn) v256Swizzle() error {
	f.flush()
	scratch := f.allocSpillSlots(6)
	outBase, tmpBase := scratch, scratch+4
	idx, src := f.popValue(), f.popValue()
	for half := 0; half < 2; half++ {
		idxSlot := idx.st.slot + half*2
		f.pushV128Slot(src.st.slot)
		f.pushV128Slot(idxSlot)
		if err := f.emitV128Half(14); err != nil {
			return err
		}
		f.storeTopV128(outBase + half*2)

		x := f.allocFReg(0)
		f.fpinned = f.fpinned.add(x)
		f.a.VMovdquLoadDisp(x, RSP, f.spillOff(idxSlot))
		bias := f.v128ConstReg(0x1010101010101010, 0x1010101010101010)
		f.a.VPsubb(x, x, bias)
		f.releaseF(bias)
		f.fpinned = f.fpinned.remove(x)
		f.a.VMovdquStoreDisp(RSP, f.spillOff(tmpBase), x)
		f.releaseF(x)

		f.pushV128Slot(src.st.slot + 2)
		f.pushV128Slot(tmpBase)
		if err := f.emitV128Half(14); err != nil {
			return err
		}
		hi := f.materializeV128(f.popValue())
		lo := f.allocFReg(maskOf(hi))
		f.a.VMovdquLoadDisp(lo, RSP, f.spillOff(outBase+half*2))
		f.a.VPor(lo, lo, hi)
		f.releaseF(hi)
		f.a.VMovdquStoreDisp(RSP, f.spillOff(outBase+half*2), lo)
		f.releaseF(lo)
	}
	f.pushV256Slot(outBase)
	return nil
}

func (f *fn) v256CrossUnary(lowOp, highOp uint32, sourceHalf int) error {
	f.flush()
	out := f.allocSpillSlots(4)
	a := f.popValue()
	for half, op := range [2]uint32{lowOp, highOp} {
		f.pushV128Slot(a.st.slot + sourceHalf*2)
		if err := f.emitV128Half(op); err != nil {
			return err
		}
		f.storeTopV128(out + half*2)
	}
	f.pushV256Slot(out)
	return nil
}

func (f *fn) v256CrossExtmul(lowOp, highOp uint32, sourceHalf int) error {
	f.flush()
	out := f.allocSpillSlots(4)
	b, a := f.popValue(), f.popValue()
	for half, op := range [2]uint32{lowOp, highOp} {
		f.pushV128Slot(a.st.slot + sourceHalf*2)
		f.pushV128Slot(b.st.slot + sourceHalf*2)
		if err := f.emitV128Half(op); err != nil {
			return err
		}
		f.storeTopV128(out + half*2)
	}
	f.pushV256Slot(out)
	return nil
}

func (f *fn) v256Narrow(sub uint32) error {
	f.flush()
	out := f.allocSpillSlots(4)
	b, a := f.popValue(), f.popValue()
	for half, input := range [2]int{a.st.slot, b.st.slot} {
		f.pushV128Slot(input)
		f.pushV128Slot(input + 2)
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		f.storeTopV128(out + half*2)
	}
	f.pushV256Slot(out)
	return nil
}

func (f *fn) v256PackLow64Unary(sub uint32) error {
	f.flush()
	out := f.allocSpillSlots(4)
	a := f.popValue()
	for half := 0; half < 2; half++ {
		f.pushV128Slot(a.st.slot + half*2)
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		x := f.materializeV128(f.popValue())
		t := f.allocReg(0)
		f.a.MovXmmToGpr(t, x, true)
		f.releaseF(x)
		f.a.Store64(RSP, f.spillOff(out+half), t)
		f.release(t)
	}
	lo := f.allocFReg(0)
	f.a.VMovdquLoadDisp(lo, RSP, f.spillOff(out))
	y := f.allocFReg(maskOf(lo))
	f.a.YPxor(y, y, y)
	f.a.YInsertI128(y, y, lo, 0)
	f.releaseF(lo)
	f.pushYReg(y)
	return nil
}

func (f *fn) v256Low128Windows(sub uint32) error {
	f.flush()
	out := f.allocSpillSlots(4)
	a := f.popValue()
	for half := 0; half < 2; half++ {
		// The second conversion consumes lanes 2/3 of the low 128 bits. A
		// one-slot-shifted XMM load places those lanes at positions zero/one.
		f.pushV128Slot(a.st.slot + half)
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		f.storeTopV128(out + half*2)
	}
	f.pushV256Slot(out)
	return nil
}

func (f *fn) v256SplitUnary(sub uint32) error {
	f.flush()
	a := f.popValue()
	base := a.st.slot
	for half := 0; half < 2; half++ {
		slot := base + half*2
		f.pushV128Slot(slot)
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		f.storeTopV128(slot)
	}
	f.pushV256Slot(base)
	return nil
}

func (f *fn) v256SplitBinary(sub uint32) error {
	f.flush()
	b, a := f.popValue(), f.popValue()
	for half := 0; half < 2; half++ {
		off := half * 2
		f.pushV128Slot(a.st.slot + off)
		f.pushV128Slot(b.st.slot + off)
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		f.storeTopV128(a.st.slot + off)
	}
	f.pushV256Slot(a.st.slot)
	return nil
}

func (f *fn) v256SplitTernary(sub uint32) error {
	f.flush()
	c, b, a := f.popValue(), f.popValue(), f.popValue()
	for half := 0; half < 2; half++ {
		off := half * 2
		f.pushV128Slot(a.st.slot + off)
		f.pushV128Slot(b.st.slot + off)
		f.pushV128Slot(c.st.slot + off)
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		f.storeTopV128(a.st.slot + off)
	}
	f.pushV256Slot(a.st.slot)
	return nil
}

func (f *fn) v256SplitSplat(sub uint32) error {
	f.flush()
	out := f.allocSpillSlots(4)
	scalar := f.popValue()
	for half := 0; half < 2; half++ {
		f.pushValue(storage{kind: stSlot, typ: scalar.st.typ, slot: scalar.st.slot})
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		f.storeTopV128(out + half*2)
	}
	f.pushV256Slot(out)
	return nil
}

func (f *fn) v256SplitShift(sub uint32) error {
	f.flush()
	out := f.allocSpillSlots(4)
	count, value := f.popValue(), f.popValue()
	for half := 0; half < 2; half++ {
		f.pushV128Slot(value.st.slot + half*2)
		f.pushValue(storage{kind: stSlot, typ: mtI32, slot: count.st.slot})
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		f.storeTopV128(out + half*2)
	}
	f.pushV256Slot(out)
	return nil
}

func (f *fn) v256SplitReduction(sub uint32, bitmaskShift int, allTrue bool) error {
	f.flush()
	a := f.popValue()
	var result Reg = regNone
	for half := 0; half < 2; half++ {
		f.pushV128Slot(a.st.slot + half*2)
		if err := f.emitV128Half(sub); err != nil {
			return err
		}
		e := f.popValue()
		r := f.materialize(e)
		if half == 0 {
			result = r
			f.pinned = f.pinned.add(result)
			continue
		}
		if bitmaskShift != 0 {
			f.a.ShiftImm(4, r, byte(bitmaskShift), false)
		}
		if allTrue {
			f.a.AluRR(0x21, result, r, false)
		} else {
			f.a.AluRR(0x09, result, r, false)
		}
		f.release(r)
	}
	f.pinned = f.pinned.remove(result)
	f.pushReg(result, mtI32)
	return nil
}

func v256MirrorUnary(sub uint32) bool {
	switch sub {
	case 14, 77, 94, 95, 96, 97, 98, 103, 104, 105, 106, 116, 117, 122,
		124, 125, 126, 127, 128, 129, 135, 136, 137, 138, 148, 160, 161,
		167, 168, 169, 170, 192, 193, 199, 200, 201, 202, 224, 225, 227,
		236, 237, 239, 248, 249, 250, 251, 252, 253, 254, 255, 256, 257,
		258, 259, 260:
		return true
	}
	return false
}

func (f *fn) emitV256YMM(sub uint32) bool {
	switch sub {
	case 35:
		f.v256Bin(f.a.YPcmpeqb)
	case 36:
		f.v256BinNot(f.a.YPcmpeqb)
	case 37:
		f.v256SignedCmp(f.a.YPcmpgtb, true, false)
	case 38:
		f.v256UnsignedCmp(f.a.YPcmpgtb, 0x8080808080808080, 0x8080808080808080, true, false)
	case 39:
		f.v256Bin(f.a.YPcmpgtb)
	case 40:
		f.v256UnsignedCmp(f.a.YPcmpgtb, 0x8080808080808080, 0x8080808080808080, false, false)
	case 41:
		f.v256SignedCmp(f.a.YPcmpgtb, false, true)
	case 42:
		f.v256UnsignedCmp(f.a.YPcmpgtb, 0x8080808080808080, 0x8080808080808080, false, true)
	case 43:
		f.v256SignedCmp(f.a.YPcmpgtb, true, true)
	case 44:
		f.v256UnsignedCmp(f.a.YPcmpgtb, 0x8080808080808080, 0x8080808080808080, true, true)
	case 45:
		f.v256Bin(f.a.YPcmpeqw)
	case 46:
		f.v256BinNot(f.a.YPcmpeqw)
	case 47:
		f.v256SignedCmp(f.a.YPcmpgtw, true, false)
	case 48:
		f.v256UnsignedCmp(f.a.YPcmpgtw, 0x8000800080008000, 0x8000800080008000, true, false)
	case 49:
		f.v256Bin(f.a.YPcmpgtw)
	case 50:
		f.v256UnsignedCmp(f.a.YPcmpgtw, 0x8000800080008000, 0x8000800080008000, false, false)
	case 51:
		f.v256SignedCmp(f.a.YPcmpgtw, false, true)
	case 52:
		f.v256UnsignedCmp(f.a.YPcmpgtw, 0x8000800080008000, 0x8000800080008000, false, true)
	case 53:
		f.v256SignedCmp(f.a.YPcmpgtw, true, true)
	case 54:
		f.v256UnsignedCmp(f.a.YPcmpgtw, 0x8000800080008000, 0x8000800080008000, true, true)
	case 55:
		f.v256Bin(f.a.YPcmpeqd)
	case 56:
		f.v256BinNot(f.a.YPcmpeqd)
	case 57:
		f.v256SignedCmp(f.a.YPcmpgtd, true, false)
	case 58:
		f.v256UnsignedCmp(f.a.YPcmpgtd, 0x8000000080000000, 0x8000000080000000, true, false)
	case 59:
		f.v256Bin(f.a.YPcmpgtd)
	case 60:
		f.v256UnsignedCmp(f.a.YPcmpgtd, 0x8000000080000000, 0x8000000080000000, false, false)
	case 61:
		f.v256SignedCmp(f.a.YPcmpgtd, false, true)
	case 62:
		f.v256UnsignedCmp(f.a.YPcmpgtd, 0x8000000080000000, 0x8000000080000000, false, true)
	case 63:
		f.v256SignedCmp(f.a.YPcmpgtd, true, true)
	case 64:
		f.v256UnsignedCmp(f.a.YPcmpgtd, 0x8000000080000000, 0x8000000080000000, true, true)
	case 65:
		f.v256FCmp(false, vfcmpEqOQ)
	case 66:
		f.v256FCmp(false, vfcmpNeqUQ)
	case 67:
		f.v256FCmp(false, vfcmpLtOQ)
	case 68:
		f.v256FCmp(false, vfcmpGtOQ)
	case 69:
		f.v256FCmp(false, vfcmpLeOQ)
	case 70:
		f.v256FCmp(false, vfcmpGeOQ)
	case 71:
		f.v256FCmp(true, vfcmpEqOQ)
	case 72:
		f.v256FCmp(true, vfcmpNeqUQ)
	case 73:
		f.v256FCmp(true, vfcmpLtOQ)
	case 74:
		f.v256FCmp(true, vfcmpGtOQ)
	case 75:
		f.v256FCmp(true, vfcmpLeOQ)
	case 76:
		f.v256FCmp(true, vfcmpGeOQ)
	case 96:
		f.v256Unary(f.a.YPabsb)
	case 97:
		f.v256IntegerNeg(f.a.YPsubb)
	case 98:
		f.v256I8Popcnt()
	case 99:
		f.v256AllTrue(f.a.YPcmpeqb)
	case 100:
		f.v256I8Bitmask()
	case 103, 104, 105, 106:
		mode := [...]byte{roundCeil, roundFloor, roundTrunc, roundNearest}[sub-103]
		f.v256Unary(func(d, x Reg) { f.a.YFRoundPacked(d, x, false, mode) })
	case 111:
		f.v256Bin(f.a.YPaddsb)
	case 112:
		f.v256Bin(f.a.YPaddusb)
	case 114:
		f.v256Bin(f.a.YPsubsb)
	case 115:
		f.v256Bin(f.a.YPsubusb)
	case 116, 117, 122, 148:
		modes := map[uint32]byte{116: roundCeil, 117: roundFloor, 122: roundTrunc, 148: roundNearest}
		f.v256Unary(func(d, x Reg) { f.a.YFRoundPacked(d, x, true, modes[sub]) })
	case 118:
		f.v256Bin(f.a.YPminsb)
	case 119:
		f.v256Bin(f.a.YPminub)
	case 120:
		f.v256Bin(f.a.YPmaxsb)
	case 121:
		f.v256Bin(f.a.YPmaxub)
	case 123:
		f.v256Bin(f.a.YPavgb)
	case 128:
		f.v256Unary(f.a.YPabsw)
	case 129:
		f.v256IntegerNeg(f.a.YPsubw)
	case 130, 273:
		f.v256Bin(f.a.YPmulhrsw)
	case 131:
		f.v256AllTrue(f.a.YPcmpeqw)
	case 139:
		f.v256Shift(f.a.YPsllw, 15)
	case 140:
		f.v256Shift(f.a.YPsraw, 15)
	case 141:
		f.v256Shift(f.a.YPsrlw, 15)
	case 143:
		f.v256Bin(f.a.YPaddsw)
	case 144:
		f.v256Bin(f.a.YPaddusw)
	case 146:
		f.v256Bin(f.a.YPsubsw)
	case 147:
		f.v256Bin(f.a.YPsubusw)
	case 150:
		f.v256Bin(f.a.YPminsw)
	case 151:
		f.v256Bin(f.a.YPminuw)
	case 152:
		f.v256Bin(f.a.YPmaxsw)
	case 153:
		f.v256Bin(f.a.YPmaxuw)
	case 155:
		f.v256Bin(f.a.YPavgw)
	case 160:
		f.v256Unary(f.a.YPabsd)
	case 161:
		f.v256IntegerNeg(f.a.YPsubd)
	case 163:
		f.v256AllTrue(f.a.YPcmpeqd)
	case 171:
		f.v256Shift(f.a.YPslld, 31)
	case 172:
		f.v256Shift(f.a.YPsrad, 31)
	case 173:
		f.v256Shift(f.a.YPsrld, 31)
	case 182:
		f.v256Bin(f.a.YPminsd)
	case 183:
		f.v256Bin(f.a.YPminud)
	case 184:
		f.v256Bin(f.a.YPmaxsd)
	case 185:
		f.v256Bin(f.a.YPmaxud)
	case 186:
		f.v256Bin(f.a.YPmaddwd)
	case 193:
		f.v256IntegerNeg(f.a.YPsubq)
	case 195:
		f.v256AllTrue(f.a.YPcmpeqq)
	case 203:
		f.v256Shift(f.a.YPsllq, 63)
	case 205:
		f.v256Shift(f.a.YPsrlq, 63)
	case 214:
		f.v256Bin(f.a.YPcmpeqq)
	case 215:
		f.v256BinNot(f.a.YPcmpeqq)
	case 216:
		f.v256SignedCmp(f.a.YPcmpgtq, true, false)
	case 217:
		f.v256Bin(f.a.YPcmpgtq)
	case 218:
		f.v256SignedCmp(f.a.YPcmpgtq, false, true)
	case 219:
		f.v256SignedCmp(f.a.YPcmpgtq, true, true)
	case 224:
		f.v256FloatSignOp(false, 0x54, 0x7fffffff7fffffff, 0x7fffffff7fffffff)
	case 225:
		f.v256FloatSignOp(false, 0x57, 0x8000000080000000, 0x8000000080000000)
	case 227:
		f.v256Unary(func(d, x Reg) { f.a.YFPackedSqrt(d, x, false) })
	case 232:
		f.v256FloatMinMax(false, false)
	case 233:
		f.v256FloatMinMax(false, true)
	case 234:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMin(d, b, a, false) })
	case 235:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMax(d, b, a, false) })
	case 236:
		f.v256FloatSignOp(true, 0x54, 0x7fffffffffffffff, 0x7fffffffffffffff)
	case 237:
		f.v256FloatSignOp(true, 0x57, 0x8000000000000000, 0x8000000000000000)
	case 239:
		f.v256Unary(func(d, x Reg) { f.a.YFPackedSqrt(d, x, true) })
	case 244:
		f.v256FloatMinMax(true, false)
	case 245:
		f.v256FloatMinMax(true, true)
	case 246:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMin(d, b, a, true) })
	case 247:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMax(d, b, a, true) })
	case 261:
		f.v256RelaxedMadd(false, false)
	case 262:
		f.v256RelaxedMadd(false, true)
	case 263:
		f.v256RelaxedMadd(true, false)
	case 264:
		f.v256RelaxedMadd(true, true)
	case 269:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMin(d, a, b, false) })
	case 270:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMax(d, a, b, false) })
	case 271:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMin(d, a, b, true) })
	case 272:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMax(d, a, b, true) })
	default:
		return false
	}
	return true
}

func (f *fn) emitV256Mirror(sub uint32, r *wasm.Reader) error {
	if f.emitV256YMM(sub) {
		return nil
	}
	switch sub {
	case 0:
		return f.v256Load(r)
	case 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 92, 93:
		return f.v256MirrorLoad(sub, r)
	case 11:
		return f.v256Store(r)
	case 12:
		return f.v256Const(r)
	case 13:
		return f.v256Shuffle(r)
	case 14, 256:
		return f.v256Swizzle()
	case 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34:
		return f.v256MirrorLane(sub, r)
	case 84, 85, 86, 87, 88, 89, 90, 91:
		return f.v256MirrorMemLane(sub, r)
	case 15, 16, 17, 18, 19, 20:
		return f.v256SplitSplat(sub)
	case 83:
		f.v256AnyTrue()
		return nil
	case 99:
		return f.v256SplitReduction(sub, 0, true)
	case 100:
		return f.v256SplitReduction(sub, 16, false)
	case 131:
		return f.v256SplitReduction(sub, 0, true)
	case 132:
		return f.v256SplitReduction(sub, 8, false)
	case 163:
		return f.v256SplitReduction(sub, 0, true)
	case 164:
		return f.v256SplitReduction(sub, 4, false)
	case 195:
		return f.v256SplitReduction(sub, 0, true)
	case 196:
		return f.v256SplitReduction(sub, 2, false)
	case 107, 108, 109, 139, 140, 141, 171, 172, 173, 203, 204, 205:
		return f.v256SplitShift(sub)
	case 101, 102, 133, 134:
		return f.v256Narrow(sub)
	case 94, 252, 253, 259, 260:
		return f.v256PackLow64Unary(sub)
	case 95, 254, 255:
		return f.v256Low128Windows(sub)
	case 135:
		return f.v256CrossUnary(135, 136, 0)
	case 136:
		return f.v256CrossUnary(135, 136, 1)
	case 137:
		return f.v256CrossUnary(137, 138, 0)
	case 138:
		return f.v256CrossUnary(137, 138, 1)
	case 167:
		return f.v256CrossUnary(167, 168, 0)
	case 168:
		return f.v256CrossUnary(167, 168, 1)
	case 169:
		return f.v256CrossUnary(169, 170, 0)
	case 170:
		return f.v256CrossUnary(169, 170, 1)
	case 199:
		return f.v256CrossUnary(199, 200, 0)
	case 200:
		return f.v256CrossUnary(199, 200, 1)
	case 201:
		return f.v256CrossUnary(201, 202, 0)
	case 202:
		return f.v256CrossUnary(201, 202, 1)
	case 156:
		return f.v256CrossExtmul(156, 157, 0)
	case 157:
		return f.v256CrossExtmul(156, 157, 1)
	case 158:
		return f.v256CrossExtmul(158, 159, 0)
	case 159:
		return f.v256CrossExtmul(158, 159, 1)
	case 188:
		return f.v256CrossExtmul(188, 189, 0)
	case 189:
		return f.v256CrossExtmul(188, 189, 1)
	case 190:
		return f.v256CrossExtmul(190, 191, 0)
	case 191:
		return f.v256CrossExtmul(190, 191, 1)
	case 220:
		return f.v256CrossExtmul(220, 221, 0)
	case 221:
		return f.v256CrossExtmul(220, 221, 1)
	case 222:
		return f.v256CrossExtmul(222, 223, 0)
	case 223:
		return f.v256CrossExtmul(222, 223, 1)
	case 77:
		f.v256Not()
		return nil
	case 78:
		f.v256Bin(f.a.YPand)
		return nil
	case 79:
		f.v256Andnot()
		return nil
	case 80:
		f.v256Bin(f.a.YPor)
		return nil
	case 81:
		f.v256Bin(f.a.YPxor)
		return nil
	case 82, 265, 266, 267, 268:
		f.v256Bitselect()
		return nil
	case 110:
		f.v256Bin(f.a.YPaddb)
		return nil
	case 113:
		f.v256Bin(f.a.YPsubb)
		return nil
	case 142:
		f.v256Bin(f.a.YPaddw)
		return nil
	case 145:
		f.v256Bin(f.a.YPsubw)
		return nil
	case 149:
		f.v256Bin(f.a.YPmullw)
		return nil
	case 174:
		f.v256Bin(f.a.YPaddd)
		return nil
	case 177:
		f.v256Bin(f.a.YPsubd)
		return nil
	case 181:
		f.v256Bin(f.a.YPmulld)
		return nil
	case 206:
		f.v256Bin(f.a.YPaddq)
		return nil
	case 209:
		f.v256Bin(f.a.YPsubq)
		return nil
	case 228:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedAdd(d, a, b, false) })
		return nil
	case 229:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedSub(d, a, b, false) })
		return nil
	case 230:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMul(d, a, b, false) })
		return nil
	case 231:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedDiv(d, a, b, false) })
		return nil
	case 240:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedAdd(d, a, b, true) })
		return nil
	case 241:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedSub(d, a, b, true) })
		return nil
	case 242:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedMul(d, a, b, true) })
		return nil
	case 243:
		f.v256Bin(func(d, a, b Reg) { f.a.YFPackedDiv(d, a, b, true) })
		return nil
	case 261, 262, 263, 264, 275:
		return f.v256SplitTernary(sub)
	}
	if v256MirrorUnary(sub) {
		return f.v256SplitUnary(sub)
	}
	return f.v256SplitBinary(sub)
}
