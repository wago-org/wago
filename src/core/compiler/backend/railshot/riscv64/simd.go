//go:build riscv64

package riscv64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// v128Pair is the baseline RV64G representation of a WebAssembly v128. Lanes
// use WebAssembly's little-endian numbering: lo contains bits 0..63 and hi bits
// 64..127. RVV is an optional future optimization; SWAR never requires vector
// register state.
type v128Pair struct {
	lo, hi Reg
}

func (p v128Pair) mask() regMask { return maskOf(p.lo, p.hi) }

// v128ConstReg remains in fn's shared layout while constant-pair pinning is
// disabled. Keeping the shape architecture-local avoids perturbing sibling
// backends while the SWAR allocator is brought up.
type v128ConstReg struct {
	lo, hi uint64
	regs   v128Pair
}

func (f *fn) v128ConstMask() regMask                       { return 0 }
func (f *fn) pinnedV128LocalCount() int                    { return 0 }
func (f *fn) preloadV128Consts(_ []byte)                   {}
func (f *fn) v128ConstCached(_, _ uint64) (v128Pair, bool) { return v128Pair{}, false }

func (f *fn) allocV128Pair(avoid regMask) v128Pair {
	lo := f.allocReg(avoid)
	hi := f.allocReg(avoid.add(lo))
	return v128Pair{lo: lo, hi: hi}
}

func (f *fn) occupyV128(e *elem, p v128Pair) {
	f.regUser[p.lo], f.regUser[p.hi] = e, e
	if e.kind == ekDeferred && e.typ != mtNone {
		e.st.typ = e.typ
	}
	e.kind = ekValue
	e.st.kind, e.st.reg, e.st.reg2 = stReg, p.lo, p.hi
}

func (f *fn) pushV128Pair(p v128Pair) *elem {
	e := f.pushValue(storage{kind: stReg, typ: mtV128, reg: p.lo, reg2: p.hi})
	f.regUser[p.lo], f.regUser[p.hi] = e, e
	return e
}

// pushVReg keeps legacy unreachable call-result paths type-correct while v128
// call ABI conversion is staged. Public compilation still rejects SIMD, and the
// SWAR development gate only exercises signatures without v128 params/results.
func (f *fn) pushVReg(_ Reg) *elem {
	panic("riscv64: SWAR v128 call result ABI not implemented")
}

func (f *fn) releaseV128(p v128Pair) {
	f.release(p.lo)
	f.release(p.hi)
}

func (f *fn) spillV128(e *elem) {
	f.stats.addSpill()
	p := v128Pair{lo: e.st.reg, hi: e.st.reg2}
	slot := f.allocSpillSlots(2)
	f.st64(SP, f.spillOff(slot), p.lo)
	f.st64(SP, f.spillOff(slot)+8, p.hi)
	f.releaseV128(p)
	f.replaceStorage(e, storage{kind: stSlot, typ: mtV128, slot: slot})
}

func (f *fn) materializeV128(e *elem) v128Pair {
	if e.isDeferred() {
		panic("riscv64: deferred v128 op not supported")
	}
	switch e.st.kind {
	case stReg:
		return v128Pair{lo: e.st.reg, hi: e.st.reg2}
	case stConst:
		if e.st.cval != 0 {
			panic("riscv64: non-zero compact v128 constant")
		}
		p := f.allocV128Pair(0)
		f.a.MovImm64(p.lo, 0)
		f.a.MovImm64(p.hi, 0)
		f.occupyV128(e, p)
		return p
	case stSlot:
		f.stats.addReload()
		p := f.allocV128Pair(0)
		f.ld64(p.lo, SP, f.spillOff(e.st.slot))
		f.ld64(p.hi, SP, f.spillOff(e.st.slot)+8)
		f.occupyV128(e, p)
		return p
	case stLocalRef:
		p := f.allocV128Pair(0)
		f.ld64(p.lo, SP, f.localOff(e.st.idx))
		f.ld64(p.hi, SP, f.localOff(e.st.idx)+8)
		f.occupyV128(e, p)
		return p
	case stLocalReg:
		panic("riscv64: SWAR v128 local pinning is disabled")
	}
	panic("riscv64: cannot materialize v128 storage")
}

func (f *fn) stV128(base Reg, disp int32, p v128Pair) {
	f.st64(base, disp, p.lo)
	f.st64(base, disp+8, p.hi)
}

func (f *fn) v128Const(lo, hi uint64) {
	p := f.allocV128Pair(0)
	f.a.MovImm64(p.lo, lo)
	f.a.MovImm64(p.hi, hi)
	f.pushV128Pair(p)
}

func (f *fn) v128Not() {
	e := f.popValue()
	p := f.materializeV128(e)
	f.a.Mvn64(p.lo, p.lo)
	f.a.Mvn64(p.hi, p.hi)
	f.pushV128Pair(p)
}

func (f *fn) v128Binary(sub uint32) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	switch sub {
	case 78: // and
		f.a.And64(a.lo, a.lo, b.lo)
		f.a.And64(a.hi, a.hi, b.hi)
	case 79: // andnot: a & ~b
		f.a.Mvn64(b.lo, b.lo)
		f.a.Mvn64(b.hi, b.hi)
		f.a.And64(a.lo, a.lo, b.lo)
		f.a.And64(a.hi, a.hi, b.hi)
	case 80: // or
		f.a.Orr64(a.lo, a.lo, b.lo)
		f.a.Orr64(a.hi, a.hi, b.hi)
	case 81: // xor
		f.a.Eor64(a.lo, a.lo, b.lo)
		f.a.Eor64(a.hi, a.hi, b.hi)
	default:
		panic("riscv64: invalid v128 binary opcode")
	}
	f.releaseV128(b)
	f.pushV128Pair(a)
}

func (f *fn) v128Bitselect() {
	me, be, ae := f.popValue(), f.popValue(), f.popValue()
	m := f.materializeV128(me)
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	// b ^ ((a ^ b) & mask), independently for both halves.
	f.a.Eor64(a.lo, a.lo, b.lo)
	f.a.Eor64(a.hi, a.hi, b.hi)
	f.a.And64(a.lo, a.lo, m.lo)
	f.a.And64(a.hi, a.hi, m.hi)
	f.a.Eor64(a.lo, a.lo, b.lo)
	f.a.Eor64(a.hi, a.hi, b.hi)
	f.releaseV128(b)
	f.releaseV128(m)
	f.pushV128Pair(a)
}

func (f *fn) v128AnyTrue() {
	e := f.popValue()
	p := f.materializeV128(e)
	f.a.Orr64(p.lo, p.lo, p.hi)
	f.a.Snez(p.lo, p.lo)
	f.release(p.hi)
	f.pushReg(p.lo, mtI32)
}

func (f *fn) i64x2Splat() {
	e := f.popValue()
	x := f.materialize(e)
	hi := f.allocReg(maskOf(x))
	f.a.MovReg64(hi, x)
	f.pushV128Pair(v128Pair{lo: x, hi: hi})
}

func (f *fn) i64x2ExtractLane(lane byte) error {
	if lane >= 2 {
		return fmt.Errorf("riscv64: invalid i64x2 lane %d", lane)
	}
	e := f.popValue()
	p := f.materializeV128(e)
	result, discard := p.lo, p.hi
	if lane == 1 {
		result, discard = p.hi, p.lo
	}
	f.release(discard)
	f.regUser[result] = nil
	f.pushReg(result, mtI64)
	return nil
}

func (f *fn) i64x2ReplaceLane(lane byte) error {
	if lane >= 2 {
		return fmt.Errorf("riscv64: invalid i64x2 lane %d", lane)
	}
	xe, ve := f.popValue(), f.popValue()
	x := f.materialize(xe)
	p := f.materializeV128(ve)
	if lane == 0 {
		f.a.MovReg64(p.lo, x)
	} else {
		f.a.MovReg64(p.hi, x)
	}
	f.release(x)
	f.pushV128Pair(p)
	return nil
}

func (f *fn) i64x2Binary(sub uint32) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	switch sub {
	case 206:
		f.a.Add64(a.lo, a.lo, b.lo)
		f.a.Add64(a.hi, a.hi, b.hi)
	case 209:
		f.a.Sub64(a.lo, a.lo, b.lo)
		f.a.Sub64(a.hi, a.hi, b.hi)
	case 213:
		f.a.Mul64(a.lo, a.lo, b.lo)
		f.a.Mul64(a.hi, a.hi, b.hi)
	default:
		panic("riscv64: invalid i64x2 binary opcode")
	}
	f.releaseV128(b)
	f.pushV128Pair(a)
}

func swarLaneMask(width int) uint64 {
	if width == 64 {
		return ^uint64(0)
	}
	return uint64(1)<<width - 1
}

func swarRepeatLane(value uint64, width int) uint64 {
	var out uint64
	for shift := 0; shift < 64; shift += width {
		out |= (value & swarLaneMask(width)) << shift
	}
	return out
}

// packedAddSub lowers modular lane-wise add/sub without allowing carries or
// borrows to cross lane boundaries. The formulas are separately exercised with
// adversarial carry patterns in the executable SWAR tests.
func (f *fn) packedAddSub(width int, sub bool) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	low := swarRepeatLane((uint64(1)<<(width-1))-1, width)
	high := swarRepeatLane(uint64(1)<<(width-1), width)
	t := f.allocReg(a.mask().union(b.mask()))
	emit := func(x, y Reg) {
		if !sub {
			// ((x&low)+(y&low)) ^ ((x^y)&high)
			f.a.AndImm64(t, x, low)
			f.a.Eor64(x, x, y)
			f.a.AndImm64(x, x, high)
			f.a.AndImm64(y, y, low)
			f.a.Add64(t, t, y)
			f.a.Eor64(x, x, t)
			return
		}
		// ((x|high)-(y&low)) ^ (~(x^y)&high)
		f.a.Eor64(t, x, y)
		f.a.Mvn64(t, t)
		f.a.AndImm64(t, t, high)
		f.a.OrrImm64(x, x, high)
		f.a.AndImm64(y, y, low)
		f.a.Sub64(x, x, y)
		f.a.Eor64(x, x, t)
	}
	emit(a.lo, b.lo)
	emit(a.hi, b.hi)
	f.release(t)
	f.releaseV128(b)
	f.pushV128Pair(a)
}

func (f *fn) integerSplat(width int) {
	e := f.popValue()
	x := f.materialize(e)
	switch width {
	case 8:
		f.a.AndImm64(x, x, 0xff)
		t := f.allocReg(maskOf(x))
		f.a.MovImm64(t, 0x0101010101010101)
		f.a.Mul64(x, x, t)
		f.release(t)
	case 16:
		f.a.AndImm64(x, x, 0xffff)
		t := f.allocReg(maskOf(x))
		f.a.MovImm64(t, 0x0001000100010001)
		f.a.Mul64(x, x, t)
		f.release(t)
	case 32:
		t := f.allocReg(maskOf(x))
		f.a.LslImm(t, x, 32, false)
		f.a.Orr64(x, x, t)
		f.release(t)
	default:
		panic("riscv64: invalid integer splat width")
	}
	hi := f.allocReg(maskOf(x))
	f.a.MovReg64(hi, x)
	f.pushV128Pair(v128Pair{lo: x, hi: hi})
}

func (f *fn) extractLaneTo(dst Reg, p v128Pair, lane, width int, signed bool) {
	perHalf := 64 / width
	src := p.lo
	if lane >= perHalf {
		src = p.hi
		lane -= perHalf
	}
	f.a.MovReg64(dst, src)
	if shift := lane * width; shift != 0 {
		f.a.LsrImm(dst, dst, uint8(shift), false)
	}
	if signed {
		shift := uint8(64 - width)
		f.a.LslImm(dst, dst, shift, false)
		f.a.AsrImm(dst, dst, shift, false)
	} else if width < 64 {
		f.a.AndImm64(dst, dst, swarLaneMask(width))
	}
}

func (f *fn) insertLaneFrom(dst v128Pair, lane, width int, src Reg) {
	perHalf := 64 / width
	half := dst.lo
	if lane >= perHalf {
		half = dst.hi
		lane -= perHalf
	}
	f.a.AndImm64(src, src, swarLaneMask(width))
	if shift := lane * width; shift != 0 {
		f.a.LslImm(src, src, uint8(shift), false)
	}
	f.a.Orr64(half, half, src)
}

func (f *fn) integerExtractLane(width int, signed bool, lane byte) error {
	if int(lane) >= 128/width {
		return fmt.Errorf("riscv64: invalid i%dx%d lane %d", width, 128/width, lane)
	}
	e := f.popValue()
	p := f.materializeV128(e)
	out := p.lo
	f.extractLaneTo(out, p, int(lane), width, signed)
	f.release(p.hi)
	if width < 64 {
		f.a.MovReg32(out, out)
	}
	f.regUser[out] = nil
	f.pushReg(out, mtI32OrWide(width == 64))
	return nil
}

func (f *fn) integerReplaceLane(width int, lane byte) error {
	if int(lane) >= 128/width {
		return fmt.Errorf("riscv64: invalid i%dx%d lane %d", width, 128/width, lane)
	}
	xe, ve := f.popValue(), f.popValue()
	x := f.materialize(xe)
	p := f.materializeV128(ve)
	perHalf := 64 / width
	half := p.lo
	localLane := int(lane)
	if localLane >= perHalf {
		half = p.hi
		localLane -= perHalf
	}
	laneMask := swarLaneMask(width) << (localLane * width)
	f.a.AndImm64(half, half, ^laneMask)
	f.a.AndImm64(x, x, swarLaneMask(width))
	if shift := localLane * width; shift != 0 {
		f.a.LslImm(x, x, uint8(shift), false)
	}
	f.a.Orr64(half, half, x)
	f.release(x)
	f.pushV128Pair(p)
	return nil
}

func (f *fn) integerShift(width int, arithmetic, left bool) {
	ce, ve := f.popValue(), f.popValue()
	count := f.materialize(ce)
	v := f.materializeV128(ve)
	f.a.AndImm64(count, count, uint64(width-1))
	out := f.allocV128Pair(v.mask().add(count))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	t := f.allocReg(v.mask().union(out.mask()).add(count))
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(t, v, lane, width, arithmetic)
		if left {
			f.a.Lslv64(t, t, count)
		} else if arithmetic {
			f.a.Asrv64(t, t, count)
		} else {
			f.a.Lsrv64(t, t, count)
		}
		f.insertLaneFrom(out, lane, width, t)
	}
	f.release(t)
	f.release(count)
	f.releaseV128(v)
	f.pushV128Pair(out)
}

func (f *fn) integerUnary(width int, abs bool) {
	e := f.popValue()
	v := f.materializeV128(e)
	out := f.allocV128Pair(v.mask())
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(v.mask().union(out.mask()))
	t := f.allocReg(v.mask().union(out.mask()).add(x))
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, v, lane, width, true)
		f.a.Neg64(t, x)
		if abs {
			f.a.CmpImm64(x, 0)
			f.a.Csel64(x, t, x, condL)
		} else {
			f.a.MovReg64(x, t)
		}
		f.insertLaneFrom(out, lane, width, x)
	}
	f.release(t)
	f.release(x)
	f.releaseV128(v)
	f.pushV128Pair(out)
}

func (f *fn) integerAllTrue(width int) {
	e := f.popValue()
	p := f.materializeV128(e)
	ones := swarRepeatLane(1, width)
	high := swarRepeatLane(uint64(1)<<(width-1), width)
	t := f.allocReg(p.mask())
	oneReg := f.allocReg(p.mask().add(t))
	f.a.MovImm64(oneReg, ones)
	emit := func(x Reg) {
		f.a.MovReg64(t, x)
		f.a.Sub64(x, x, oneReg)
		f.a.Mvn64(t, t)
		f.a.And64(x, x, t)
		f.a.AndImm64(x, x, high)
	}
	emit(p.lo)
	emit(p.hi)
	f.a.Orr64(p.lo, p.lo, p.hi)
	f.a.Seqz(p.lo, p.lo)
	f.release(oneReg)
	f.release(t)
	f.release(p.hi)
	f.regUser[p.lo] = nil
	f.pushReg(p.lo, mtI32)
}

func (f *fn) integerBitmask(width int) {
	e := f.popValue()
	p := f.materializeV128(e)
	out := f.allocReg(p.mask())
	t := f.allocReg(p.mask().add(out))
	f.a.MovImm64(out, 0)
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(t, p, lane, width, false)
		f.a.LsrImm(t, t, uint8(width-1), false)
		f.a.AndImm64(t, t, 1)
		if lane != 0 {
			f.a.LslImm(t, t, uint8(lane), false)
		}
		f.a.Orr64(out, out, t)
	}
	f.release(t)
	f.releaseV128(p)
	f.pushReg(out, mtI32)
}

func (f *fn) emitFD(r *wasm.Reader) error {
	sub, err := r.U32()
	if err != nil {
		return err
	}
	switch sub {
	case 12: // v128.const
		var b [16]byte
		for i := range b {
			b[i], err = r.Byte()
			if err != nil {
				return err
			}
		}
		f.v128Const(binary.LittleEndian.Uint64(b[:8]), binary.LittleEndian.Uint64(b[8:]))
	case 15: // i8x16.splat
		f.integerSplat(8)
	case 16: // i16x8.splat
		f.integerSplat(16)
	case 17: // i32x4.splat
		f.integerSplat(32)
	case 18: // i64x2.splat
		f.i64x2Splat()
	case 21, 22: // i8x16.extract_lane_{s,u}
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.integerExtractLane(8, sub == 21, lane)
	case 23: // i8x16.replace_lane
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.integerReplaceLane(8, lane)
	case 24, 25: // i16x8.extract_lane_{s,u}
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.integerExtractLane(16, sub == 24, lane)
	case 26: // i16x8.replace_lane
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.integerReplaceLane(16, lane)
	case 27: // i32x4.extract_lane
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.integerExtractLane(32, false, lane)
	case 28: // i32x4.replace_lane
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.integerReplaceLane(32, lane)
	case 29: // i64x2.extract_lane
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.i64x2ExtractLane(lane)
	case 30: // i64x2.replace_lane
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.i64x2ReplaceLane(lane)
	case 77:
		f.v128Not()
	case 78, 79, 80, 81:
		f.v128Binary(sub)
	case 82:
		f.v128Bitselect()
	case 83:
		f.v128AnyTrue()
	case 96, 97: // i8x16.abs/neg
		f.integerUnary(8, sub == 96)
	case 99:
		f.integerAllTrue(8)
	case 100:
		f.integerBitmask(8)
	case 107, 108, 109: // i8x16 shifts
		f.integerShift(8, sub == 108, sub == 107)
	case 110, 113: // i8x16 add/sub
		f.packedAddSub(8, sub == 113)
	case 128, 129: // i16x8.abs/neg
		f.integerUnary(16, sub == 128)
	case 131:
		f.integerAllTrue(16)
	case 132:
		f.integerBitmask(16)
	case 139, 140, 141: // i16x8 shifts
		f.integerShift(16, sub == 140, sub == 139)
	case 142, 145: // i16x8 add/sub
		f.packedAddSub(16, sub == 145)
	case 160, 161: // i32x4.abs/neg
		f.integerUnary(32, sub == 160)
	case 163:
		f.integerAllTrue(32)
	case 164:
		f.integerBitmask(32)
	case 171, 172, 173: // i32x4 shifts
		f.integerShift(32, sub == 172, sub == 171)
	case 174, 177: // i32x4 add/sub
		f.packedAddSub(32, sub == 177)
	case 192, 193: // i64x2.abs/neg
		f.integerUnary(64, sub == 192)
	case 195:
		f.integerAllTrue(64)
	case 196:
		f.integerBitmask(64)
	case 203, 204, 205: // i64x2 shifts
		f.integerShift(64, sub == 204, sub == 203)
	case 206, 209, 213:
		f.i64x2Binary(sub)
	default:
		return fmt.Errorf("riscv64: SWAR SIMD opcode %d is not implemented", sub)
	}
	return nil
}
