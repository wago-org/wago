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
// register state. Packed formulas and scalar decomposition patterns use
// JairusSW/as-simd's MIT-licensed v128_swar implementation as a primary
// reference; see THIRD_PARTY_NOTICES.md.
type v128Pair struct {
	lo, hi Reg
}

func (p v128Pair) mask() regMask { return maskOf(p.lo, p.hi) }

// v128ConstReg holds a repeated SWAR constant in a reserved GPR pair.
type v128ConstReg struct {
	lo, hi uint64
	regs   v128Pair
}

// RV64 caches v128 constants in GPR pairs, not FP/vector registers. The pairs
// are blocked through f.reserved; the FP allocator therefore has no v128 mask.
func (f *fn) v128ConstMask() regMask { return 0 }
func (f *fn) pinnedV128LocalCount() int {
	n := 0
	for i := range f.locals {
		if _, ok := f.pinV128Pair(i); ok {
			n++
		}
	}
	return n
}
func (f *fn) preloadV128Consts(code []byte) {
	if f.usesCalls || !v128ConstCacheEnabled || f.pinnedV128LocalCount() != 0 {
		return
	}
	var cand [8]struct {
		lo, hi uint64
		n      int
	}
	nCand := 0
	r := wasm.NewReader(code)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return
		}
		if op != 0xfd {
			if err := wasm.SkipInstructionImmediate(r, op); err != nil {
				return
			}
			continue
		}
		afterPrefix := r.Offset()
		sub, err := r.U32()
		if err != nil {
			return
		}
		if sub == 12 {
			lo, err := r.LEU64()
			if err != nil {
				return
			}
			hi, err := r.LEU64()
			if err != nil {
				return
			}
			found := false
			for i := 0; i < nCand; i++ {
				if cand[i].lo == lo && cand[i].hi == hi {
					cand[i].n++
					found = true
					break
				}
			}
			if !found && nCand < len(cand) {
				cand[nCand].lo, cand[nCand].hi, cand[nCand].n = lo, hi, 1
				nCand++
			}
			continue
		}
		if err := r.JumpTo(afterPrefix); err != nil {
			return
		}
		if err := wasm.SkipInstructionImmediate(r, op); err != nil {
			return
		}
	}
	// Two reserved GPRs are expensive on RV64, so cache only one constant that is
	// present at least three times in the static body. This reduces each later
	// materialization to two moves while retaining ample SWAR scratch capacity.
	for i := 0; i < nCand; i++ {
		if cand[i].n < 3 || (cand[i].lo == 0 && cand[i].hi == 0) {
			continue
		}
		p := f.allocV128Pair(0)
		f.a.MovImm64(p.lo, cand[i].lo)
		f.a.MovImm64(p.hi, cand[i].hi)
		f.reserved = f.reserved.union(p.mask())
		f.vconsts = append(f.vconsts, v128ConstReg{lo: cand[i].lo, hi: cand[i].hi, regs: p})
		break
	}
}

func (f *fn) v128ConstCached(lo, hi uint64) (v128Pair, bool) {
	for _, c := range f.vconsts {
		if c.lo == lo && c.hi == hi {
			return c.regs, true
		}
	}
	return v128Pair{}, false
}

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
		src := v128Pair{lo: e.st.reg, hi: e.st.reg2}
		p := f.allocV128Pair(src.mask())
		f.a.MovReg64(p.lo, src.lo)
		f.a.MovReg64(p.hi, src.hi)
		f.occupyV128(e, p)
		return p
	}
	panic("riscv64: cannot materialize v128 storage")
}

func (f *fn) stV128(base Reg, disp int32, p v128Pair) {
	f.st64(base, disp, p.lo)
	f.st64(base, disp+8, p.hi)
}

func (f *fn) v128Const(lo, hi uint64) {
	p := f.allocV128Pair(0)
	if c, ok := f.v128ConstCached(lo, hi); ok {
		f.a.MovReg64(p.lo, c.lo)
		f.a.MovReg64(p.hi, c.hi)
	} else {
		f.a.MovImm64(p.lo, lo)
		f.a.MovImm64(p.hi, hi)
	}
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

func (f *fn) i8x16Shuffle(lanes [16]byte) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	t := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	for lane, selector := range lanes {
		src, srcLane := a, int(selector)
		if selector >= 16 {
			src, srcLane = b, int(selector)-16
		}
		f.extractLaneTo(t, src, srcLane, 8, false)
		f.insertLaneFrom(out, lane, 8, t)
	}
	f.release(t)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) i8x16Swizzle() {
	ie, de := f.popValue(), f.popValue()
	indices := f.materializeV128(ie)
	data := f.materializeV128(de)
	out := f.allocV128Pair(indices.mask().union(data.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	idx := f.allocReg(indices.mask().union(data.mask()).union(out.mask()))
	shift := f.allocReg(indices.mask().union(data.mask()).union(out.mask()).add(idx))
	value := f.allocReg(indices.mask().union(data.mask()).union(out.mask()).add(idx).add(shift))
	for lane := 0; lane < 16; lane++ {
		f.extractLaneTo(idx, indices, lane, 8, false)
		f.a.AndImm64(shift, idx, 7)
		f.a.LslImm(shift, shift, 3, false)
		f.a.CmpImm64(idx, 8)
		f.a.Csel64(value, data.hi, data.lo, condAE)
		f.a.Lsrv64(value, value, shift)
		f.a.AndImm64(value, value, 0xff)
		f.a.CmpImm64(idx, 16)
		f.a.Csel64(value, value, ZR, condB)
		f.insertLaneFrom(out, lane, 8, value)
	}
	f.release(value)
	f.release(shift)
	f.release(idx)
	f.releaseV128(data)
	f.releaseV128(indices)
	f.pushV128Pair(out)
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
	f.pinned = f.pinned.add(x)
	p := f.materializeV128(ve)
	f.pinned = f.pinned.remove(x)
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

func (f *fn) floatSplat(f64 bool) {
	e := f.popValue()
	fr := f.materializeF(e)
	x := f.allocReg(0)
	f.a.FmovToGpr(x, fr, f64)
	if !f64 {
		f.a.MovReg32(x, x) // discard RV64 NaN-boxing bits before packing two f32 lanes
	}
	f.releaseF(fr)
	f.pushReg(x, mtI32OrWide(f64))
	if f64 {
		f.i64x2Splat()
	} else {
		f.integerSplat(32)
	}
}

func (f *fn) floatExtractLane(f64 bool, lane byte) error {
	width, lanes := 32, 4
	if f64 {
		width, lanes = 64, 2
	}
	if int(lane) >= lanes {
		return fmt.Errorf("riscv64: invalid f%dx%d lane %d", width, lanes, lane)
	}
	p := f.materializeV128(f.popValue())
	x := p.lo
	f.extractLaneTo(x, p, int(lane), width, false)
	f.release(p.hi)
	fr := f.allocFReg(0)
	f.a.FmovFromGpr(fr, x, f64)
	f.release(x)
	f.pushFReg(fr, mtOf2(f64))
	return nil
}

func (f *fn) floatReplaceLane(f64 bool, lane byte) error {
	width, lanes := 32, 4
	if f64 {
		width, lanes = 64, 2
	}
	if int(lane) >= lanes {
		return fmt.Errorf("riscv64: invalid f%dx%d lane %d", width, lanes, lane)
	}
	se, ve := f.popValue(), f.popValue()
	fr := f.materializeF(se)
	x := f.allocReg(0)
	f.a.FmovToGpr(x, fr, f64)
	f.releaseF(fr)
	f.pinned = f.pinned.add(x)
	p := f.materializeV128(ve)
	f.pinned = f.pinned.remove(x)
	f.replaceLaneFromReg(p, int(lane), width, x)
	f.release(x)
	f.pushV128Pair(p)
	return nil
}

func (f *fn) floatUnary(f64 bool, op uint32) {
	p := f.materializeV128(f.popValue())
	if op == 224 || op == 236 { // abs is a pure sign-bit clear
		mask := ^uint64(1 << 63)
		if !f64 {
			mask = 0x7fffffff7fffffff
		}
		f.a.AndImm64(p.lo, p.lo, mask)
		f.a.AndImm64(p.hi, p.hi, mask)
		f.pushV128Pair(p)
		return
	}
	if op == 225 || op == 237 { // neg is a pure sign-bit toggle
		mask := uint64(1 << 63)
		if !f64 {
			mask = 0x8000000080000000
		}
		f.a.EorImm64(p.lo, p.lo, mask)
		f.a.EorImm64(p.hi, p.hi, mask)
		f.pushV128Pair(p)
		return
	}
	width := 32
	if f64 {
		width = 64
	}
	out := f.allocV128Pair(p.mask())
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(p.mask().union(out.mask()))
	fr := f.allocFReg(0)
	rounding := op == 103 || op == 104 || op == 105 || op == 106 || op == 116 || op == 117 || op == 122 || op == 148
	zeroTest := regNone
	if rounding {
		zeroTest = f.allocReg(p.mask().union(out.mask()).add(x))
	}
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, p, lane, width, false)
		var preserveZero int
		if rounding {
			magMask := ^uint64(1 << 63)
			if !f64 {
				magMask = 0x7fffffff
			}
			f.a.AndImm64(zeroTest, x, magMask)
			preserveZero = f.a.Cbz64(zeroTest)
		}
		f.a.FmovFromGpr(fr, x, f64)
		switch op {
		case 227, 239:
			f.a.Fsqrt(fr, fr, f64)
		case 103, 116:
			f.a.Frint(fr, fr, f64, roundCeil)
		case 104, 117:
			f.a.Frint(fr, fr, f64, roundFloor)
		case 105, 122:
			f.a.Frint(fr, fr, f64, roundTrunc)
		case 106, 148:
			f.a.Frint(fr, fr, f64, roundNearest)
		default:
			panic("riscv64: invalid float SIMD unary opcode")
		}
		f.a.FmovToGpr(x, fr, f64)
		if rounding {
			f.a.PatchBranch19(preserveZero, f.a.Len())
		}
		f.insertLaneFrom(out, lane, width, x)
	}
	if zeroTest != regNone {
		f.release(zeroTest)
	}
	f.releaseF(fr)
	f.release(x)
	f.releaseV128(p)
	f.pushV128Pair(out)
}

func (f *fn) floatBinary(f64 bool, op uint32) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	fa := f.allocFReg(0)
	fb := f.allocFReg(maskOf(fa))
	width := 32
	if f64 {
		width = 64
	}
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, a, lane, width, false)
		f.a.FmovFromGpr(fa, x, f64)
		f.extractLaneTo(x, b, lane, width, false)
		f.a.FmovFromGpr(fb, x, f64)
		switch op {
		case 228, 240:
			f.a.Fadd(fa, fa, fb, f64)
		case 229, 241:
			f.a.Fsub(fa, fa, fb, f64)
		case 230, 242:
			f.a.Fmul(fa, fa, fb, f64)
		case 231, 243:
			f.a.Fdiv(fa, fa, fb, f64)
		case 232, 244, 269, 271:
			f.scalarFMinMaxInto(fa, fb, f64, false)
		case 233, 245, 270, 272:
			f.scalarFMinMaxInto(fa, fb, f64, true)
		default:
			panic("riscv64: invalid float SIMD binary opcode")
		}
		f.a.FmovToGpr(x, fa, f64)
		f.insertLaneFrom(out, lane, width, x)
	}
	f.releaseF(fb)
	f.releaseF(fa)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) floatPMinMax(f64, max bool) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	xa := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	xb := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(xa))
	fa := f.allocFReg(0)
	fb := f.allocFReg(maskOf(fa))
	width := 32
	if f64 {
		width = 64
	}
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(xa, a, lane, width, false)
		f.extractLaneTo(xb, b, lane, width, false)
		f.a.FmovFromGpr(fa, xa, f64)
		f.a.FmovFromGpr(fb, xb, f64)
		if max {
			f.a.Fcmp(fa, fb, f64)
		} else {
			f.a.Fcmp(fb, fa, f64)
		}
		f.a.Csel64(xa, xb, xa, condL) // choose b only when strictly better
		f.insertLaneFrom(out, lane, width, xa)
	}
	f.releaseF(fb)
	f.releaseF(fa)
	f.release(xb)
	f.release(xa)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) floatDemotePromote(promote bool) {
	p := f.materializeV128(f.popValue())
	out := f.allocV128Pair(p.mask())
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(p.mask().union(out.mask()))
	fr := f.allocFReg(0)
	for lane := 0; lane < 2; lane++ {
		if promote {
			f.extractLaneTo(x, p, lane, 32, false)
			f.a.FmovFromGpr(fr, x, false)
			f.a.FcvtS2D(fr, fr)
			f.a.FmovToGpr(x, fr, true)
			f.insertLaneFrom(out, lane, 64, x)
		} else {
			f.extractLaneTo(x, p, lane, 64, false)
			f.a.FmovFromGpr(fr, x, true)
			f.a.FcvtD2S(fr, fr)
			f.a.FmovToGpr(x, fr, false)
			f.insertLaneFrom(out, lane, 32, x)
		}
	}
	f.releaseF(fr)
	f.release(x)
	f.releaseV128(p)
	f.pushV128Pair(out)
}

func (f *fn) floatTruncSat(f64src, signed bool) {
	p := f.materializeV128(f.popValue())
	out := f.allocV128Pair(p.mask())
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(p.mask().union(out.mask()))
	fr := f.allocFReg(0)
	// Scalar saturation materializes threshold constants internally. Pin every
	// detached pair/temp register so those nested allocations cannot reclaim a
	// live SIMD half, lane result, or source FP register.
	f.pinned = f.pinned.union(p.mask()).union(out.mask()).add(x)
	f.fpinned = f.fpinned.add(fr)
	width, lanes := 32, 4
	if f64src {
		width, lanes = 64, 2
	}
	for lane := 0; lane < lanes; lane++ {
		f.extractLaneTo(x, p, lane, width, false)
		f.a.FmovFromGpr(fr, x, f64src)
		if signed {
			f.truncSatSigned(fr, x, f64src, false)
		} else {
			f.truncSatU32(fr, x, f64src)
		}
		f.insertLaneFrom(out, lane, 32, x)
	}
	f.fpinned = f.fpinned.remove(fr)
	f.pinned = f.pinned.remove(p.lo).remove(p.hi).remove(out.lo).remove(out.hi).remove(x)
	f.releaseF(fr)
	f.release(x)
	f.releaseV128(p)
	f.pushV128Pair(out)
}

func (f *fn) integerToFloat(f64dst, signed bool) {
	p := f.materializeV128(f.popValue())
	out := f.allocV128Pair(p.mask())
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(p.mask().union(out.mask()))
	fr := f.allocFReg(0)
	lanes := 4
	if f64dst {
		lanes = 2
	}
	for lane := 0; lane < lanes; lane++ {
		f.extractLaneTo(x, p, lane, 32, signed)
		if signed {
			f.a.Scvtf(fr, x, f64dst, false)
		} else {
			f.a.Ucvtf(fr, x, f64dst, false)
		}
		f.a.FmovToGpr(x, fr, f64dst)
		width := 32
		if f64dst {
			width = 64
		}
		f.insertLaneFrom(out, lane, width, x)
	}
	f.releaseF(fr)
	f.release(x)
	f.releaseV128(p)
	f.pushV128Pair(out)
}

func (f *fn) relaxedFloatMadd(f64, neg bool) {
	ce, be, ae := f.popValue(), f.popValue(), f.popValue()
	c := f.materializeV128(ce)
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()).union(c.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(c.mask()).union(out.mask()))
	fa := f.allocFReg(0)
	fb := f.allocFReg(maskOf(fa))
	width := 32
	if f64 {
		width = 64
	}
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, a, lane, width, false)
		f.a.FmovFromGpr(fa, x, f64)
		f.extractLaneTo(x, b, lane, width, false)
		f.a.FmovFromGpr(fb, x, f64)
		f.a.Fmul(fa, fa, fb, f64)
		if neg {
			f.a.Fneg(fa, fa, f64)
		}
		f.extractLaneTo(x, c, lane, width, false)
		f.a.FmovFromGpr(fb, x, f64)
		f.a.Fadd(fa, fa, fb, f64)
		f.a.FmovToGpr(x, fa, f64)
		f.insertLaneFrom(out, lane, width, x)
	}
	f.releaseF(fb)
	f.releaseF(fa)
	f.release(x)
	f.releaseV128(c)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) floatCompare(f64 bool, cond Cond) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	fa := f.allocFReg(0)
	fb := f.allocFReg(maskOf(fa))
	width := 32
	if f64 {
		width = 64
	}
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, a, lane, width, false)
		f.a.FmovFromGpr(fa, x, f64)
		f.extractLaneTo(x, b, lane, width, false)
		f.a.FmovFromGpr(fb, x, f64)
		f.a.Fcmp(fa, fb, f64)
		f.a.Cset64(x, cond)
		f.a.Neg64(x, x)
		f.insertLaneFrom(out, lane, width, x)
	}
	f.releaseF(fb)
	f.releaseF(fa)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
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
	f.pinned = f.pinned.add(x)
	p := f.materializeV128(ve)
	f.pinned = f.pinned.remove(x)
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

func (f *fn) integerCompare(width int, cond Cond, signed bool) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, a, lane, width, signed)
		f.extractLaneTo(y, b, lane, width, signed)
		f.a.CmpReg64(x, y)
		f.a.Cset64(x, cond)
		f.a.Neg64(x, x) // true => all one bits in the destination lane
		f.insertLaneFrom(out, lane, width, x)
	}
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) integerMinMax(width int, signed, max bool) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	cond := condB
	if signed {
		cond = condL
	}
	if max {
		cond = cond.Invert()
	}
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, a, lane, width, signed)
		f.extractLaneTo(y, b, lane, width, signed)
		f.a.CmpReg64(x, y)
		f.a.Csel64(x, x, y, cond) // min: a<b; max: a>=b
		f.insertLaneFrom(out, lane, width, x)
	}
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) integerAvgrU(width int) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, a, lane, width, false)
		f.extractLaneTo(y, b, lane, width, false)
		f.a.Add64(x, x, y)
		f.a.AddImm64(x, x, 1)
		f.a.LsrImm(x, x, 1, false)
		f.insertLaneFrom(out, lane, width, x)
	}
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) integerMul(width int) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, a, lane, width, false)
		f.extractLaneTo(y, b, lane, width, false)
		f.a.Mul64(x, x, y)
		f.insertLaneFrom(out, lane, width, x)
	}
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) i8x16Popcnt() {
	e := f.popValue()
	p := f.materializeV128(e)
	t := f.allocReg(p.mask())
	emit := func(x Reg) {
		f.a.LsrImm(t, x, 1, false)
		f.a.AndImm64(t, t, 0x5555555555555555)
		f.a.Sub64(x, x, t)
		f.a.LsrImm(t, x, 2, false)
		f.a.AndImm64(t, t, 0x3333333333333333)
		f.a.AndImm64(x, x, 0x3333333333333333)
		f.a.Add64(x, x, t)
		f.a.LsrImm(t, x, 4, false)
		f.a.Add64(x, x, t)
		f.a.AndImm64(x, x, 0x0f0f0f0f0f0f0f0f)
	}
	emit(p.lo)
	emit(p.hi)
	f.release(t)
	f.pushV128Pair(p)
}

func (f *fn) clampIntegerLane(x, bound Reg, width int, signed bool) {
	if signed {
		max := uint64(1)<<(width-1) - 1
		min := -int64(uint64(1) << (width - 1))
		f.a.MovImm64(bound, max)
		f.a.CmpReg64(x, bound)
		f.a.Csel64(x, bound, x, condG)
		f.a.MovImm64(bound, uint64(min))
		f.a.CmpReg64(x, bound)
		f.a.Csel64(x, bound, x, condL)
		return
	}
	f.a.MovImm64(bound, swarLaneMask(width))
	f.a.CmpReg64(x, bound)
	f.a.Csel64(x, bound, x, condA)
}

func (f *fn) integerSaturating(width int, signed, sub bool) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	bound := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x).add(y))
	for lane := 0; lane < 128/width; lane++ {
		f.extractLaneTo(x, a, lane, width, signed)
		f.extractLaneTo(y, b, lane, width, signed)
		if sub && !signed {
			f.a.CmpReg64(x, y)
			f.a.Sub64(bound, x, y)
			f.a.Csel64(x, ZR, bound, condB)
		} else {
			if sub {
				f.a.Sub64(x, x, y)
			} else {
				f.a.Add64(x, x, y)
			}
			f.clampIntegerLane(x, bound, width, signed)
		}
		f.insertLaneFrom(out, lane, width, x)
	}
	f.release(bound)
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) integerNarrow(srcWidth int, signedOut bool) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	dstWidth := srcWidth / 2
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	bound := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	lanes := 128 / srcWidth
	for lane := 0; lane < lanes*2; lane++ {
		src, srcLane := a, lane
		if lane >= lanes {
			src, srcLane = b, lane-lanes
		}
		f.extractLaneTo(x, src, srcLane, srcWidth, true)
		if !signedOut {
			f.a.CmpImm64(x, 0)
			f.a.Csel64(x, ZR, x, condL)
		}
		f.clampIntegerLane(x, bound, dstWidth, signedOut)
		f.insertLaneFrom(out, lane, dstWidth, x)
	}
	f.release(bound)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) integerExtend(srcWidth int, signed, high bool) {
	e := f.popValue()
	v := f.materializeV128(e)
	dstWidth := srcWidth * 2
	out := f.allocV128Pair(v.mask())
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(v.mask().union(out.mask()))
	lanes := 128 / dstWidth
	start := 0
	if high {
		start = lanes
	}
	for lane := 0; lane < lanes; lane++ {
		f.extractLaneTo(x, v, start+lane, srcWidth, signed)
		f.insertLaneFrom(out, lane, dstWidth, x)
	}
	f.release(x)
	f.releaseV128(v)
	f.pushV128Pair(out)
}

func (f *fn) integerExtmul(srcWidth int, signed, high bool) {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	dstWidth := srcWidth * 2
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	lanes := 128 / dstWidth
	start := 0
	if high {
		start = lanes
	}
	for lane := 0; lane < lanes; lane++ {
		f.extractLaneTo(x, a, start+lane, srcWidth, signed)
		f.extractLaneTo(y, b, start+lane, srcWidth, signed)
		f.a.Mul64(x, x, y)
		f.insertLaneFrom(out, lane, dstWidth, x)
	}
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) integerExtaddPairwise(srcWidth int, signed bool) {
	e := f.popValue()
	v := f.materializeV128(e)
	dstWidth := srcWidth * 2
	out := f.allocV128Pair(v.mask())
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(v.mask().union(out.mask()))
	y := f.allocReg(v.mask().union(out.mask()).add(x))
	for lane := 0; lane < 128/dstWidth; lane++ {
		f.extractLaneTo(x, v, lane*2, srcWidth, signed)
		f.extractLaneTo(y, v, lane*2+1, srcWidth, signed)
		f.a.Add64(x, x, y)
		f.insertLaneFrom(out, lane, dstWidth, x)
	}
	f.release(y)
	f.release(x)
	f.releaseV128(v)
	f.pushV128Pair(out)
}

func (f *fn) i32x4DotI16x8S() {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	sum := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x).add(y))
	for lane := 0; lane < 4; lane++ {
		f.extractLaneTo(x, a, lane*2, 16, true)
		f.extractLaneTo(y, b, lane*2, 16, true)
		f.a.Mul64(sum, x, y)
		f.extractLaneTo(x, a, lane*2+1, 16, true)
		f.extractLaneTo(y, b, lane*2+1, 16, true)
		f.a.Mul64(x, x, y)
		f.a.Add64(sum, sum, x)
		f.insertLaneFrom(out, lane, 32, sum)
	}
	f.release(sum)
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) relaxedDotI8x16I7x16S() {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	sum := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x).add(y))
	for lane := 0; lane < 8; lane++ {
		f.extractLaneTo(x, a, lane*2, 8, true)
		f.extractLaneTo(y, b, lane*2, 8, true)
		f.a.Mul64(sum, x, y)
		f.extractLaneTo(x, a, lane*2+1, 8, true)
		f.extractLaneTo(y, b, lane*2+1, 8, true)
		f.a.Mul64(x, x, y)
		f.a.Add64(sum, sum, x)
		f.insertLaneFrom(out, lane, 16, sum)
	}
	f.release(sum)
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) relaxedDotI8x16I7x16AddS() {
	ce, be, ae := f.popValue(), f.popValue(), f.popValue()
	c := f.materializeV128(ce)
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()).union(c.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(c.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(c.mask()).union(out.mask()).add(x))
	sum := f.allocReg(a.mask().union(b.mask()).union(c.mask()).union(out.mask()).add(x).add(y))
	for lane := 0; lane < 4; lane++ {
		f.extractLaneTo(sum, c, lane, 32, true)
		for i := 0; i < 4; i++ {
			f.extractLaneTo(x, a, lane*4+i, 8, true)
			f.extractLaneTo(y, b, lane*4+i, 8, true)
			f.a.Mul64(x, x, y)
			f.a.Add64(sum, sum, x)
		}
		f.insertLaneFrom(out, lane, 32, sum)
	}
	f.release(sum)
	f.release(y)
	f.release(x)
	f.releaseV128(c)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func (f *fn) i16x8Q15mulrSatS() {
	be, ae := f.popValue(), f.popValue()
	b := f.materializeV128(be)
	a := f.materializeV128(ae)
	out := f.allocV128Pair(a.mask().union(b.mask()))
	f.a.MovImm64(out.lo, 0)
	f.a.MovImm64(out.hi, 0)
	x := f.allocReg(a.mask().union(b.mask()).union(out.mask()))
	y := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x))
	bound := f.allocReg(a.mask().union(b.mask()).union(out.mask()).add(x).add(y))
	for lane := 0; lane < 8; lane++ {
		f.extractLaneTo(x, a, lane, 16, true)
		f.extractLaneTo(y, b, lane, 16, true)
		f.a.Mul64(x, x, y)
		f.a.AddImm64(x, x, 0x4000)
		f.a.AsrImm(x, x, 15, false)
		f.clampIntegerLane(x, bound, 16, true)
		f.insertLaneFrom(out, lane, 16, x)
	}
	f.release(bound)
	f.release(y)
	f.release(x)
	f.releaseV128(b)
	f.releaseV128(a)
	f.pushV128Pair(out)
}

func integerCompareSpec(sub, base uint32) (cond Cond, signed bool) {
	switch sub - base {
	case 0:
		return condE, false
	case 1:
		return condNE, false
	case 2:
		return condL, true
	case 3:
		return condB, false
	case 4:
		return condG, true
	case 5:
		return condA, false
	case 6:
		return condLE, true
	case 7:
		return condBE, false
	case 8:
		return condGE, true
	case 9:
		return condAE, false
	default:
		panic("riscv64: invalid integer SIMD comparison")
	}
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
	f.pinned = f.pinned.add(ea)
	p := f.allocV128Pair(maskOf(ea))
	f.pinned = f.pinned.remove(ea)
	f.a.LoadIdx(p.lo, linMemReg, ea, disp, 8, false, true)
	f.a.LoadIdx(p.hi, linMemReg, ea, disp+8, 8, false, true)
	if eaOwned {
		f.release(ea)
	}
	f.pushV128Pair(p)
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
	f.pinned = f.pinned.add(ea)
	p := f.allocV128Pair(maskOf(ea))
	f.pinned = f.pinned.remove(ea)
	f.a.LoadIdx(p.lo, linMemReg, ea, disp, 8, false, true)
	f.a.MovImm64(p.hi, 0)
	if eaOwned {
		f.release(ea)
	}
	f.pushV128Pair(p)
	switch sub {
	case 1, 2:
		f.integerExtend(8, sub == 1, false)
	case 3, 4:
		f.integerExtend(16, sub == 3, false)
	case 5, 6:
		f.integerExtend(32, sub == 5, false)
	default:
		panic("riscv64: invalid SIMD load-extend opcode")
	}
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
	panic("riscv64: invalid SIMD load-splat opcode")
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
	f.pinned = f.pinned.add(ea)
	x := f.allocReg(maskOf(ea))
	f.pinned = f.pinned.remove(ea)
	f.a.LoadIdx(x, linMemReg, ea, disp, size, false, size == 8)
	if eaOwned {
		f.release(ea)
	}
	f.pushReg(x, mtI32OrWide(size == 8))
	if size == 8 {
		f.i64x2Splat()
	} else {
		f.integerSplat(size * 8)
	}
	return nil
}

func simdLoadZeroSize(sub uint32) int {
	switch sub {
	case 92:
		return 4
	case 93:
		return 8
	}
	panic("riscv64: invalid SIMD load-zero opcode")
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
	f.pinned = f.pinned.add(ea)
	p := f.allocV128Pair(maskOf(ea))
	f.pinned = f.pinned.remove(ea)
	f.a.LoadIdx(p.lo, linMemReg, ea, disp, size, false, size == 8)
	f.a.MovImm64(p.hi, 0)
	if eaOwned {
		f.release(ea)
	}
	f.pushV128Pair(p)
	return nil
}

// Guard pages make a single scalar access atomic with respect to Wasm traps,
// but a split 16-byte store could otherwise write its low half before the high
// half faults. Preflight the complete Wasm width before either half is stored.
func (f *fn) preflightGuardSplitStore(ea Reg, disp int32, size int) {
	if !f.guardMode {
		return
	}
	f.pinned = f.pinned.add(ea)
	t := f.allocReg(maskOf(ea))
	f.leaDisp(t, ea, disp+int32(size), true)
	mb := f.allocReg(maskOf(ea, t))
	f.ld32(mb, linMemReg, -int32(bdCurBytes))
	f.cmpRR(t, mb, true)
	f.trapIf(condA, trapMemOOB)
	f.release(mb)
	f.release(t)
	f.pinned = f.pinned.remove(ea)
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
	p := f.materializeV128(f.popValue())
	f.pinned = f.pinned.union(p.mask())
	ea, eaOwned, _, disp := f.memAddr(off, 16, true)
	f.preflightGuardSplitStore(ea, disp, 16)
	f.a.StoreIdx(linMemReg, ea, p.lo, disp, 8)
	f.a.StoreIdx(linMemReg, ea, p.hi, disp+8, 8)
	f.pinned = f.pinned.remove(p.lo).remove(p.hi)
	if eaOwned {
		f.release(ea)
	}
	f.releaseV128(p)
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
	panic("riscv64: invalid SIMD lane memory opcode")
}

func (f *fn) replaceLaneFromReg(p v128Pair, lane, width int, x Reg) {
	perHalf := 64 / width
	half := p.lo
	localLane := lane
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
	if int(lane) >= 16/size {
		return fmt.Errorf("riscv64: invalid v128.load%d_lane lane %d", size*8, lane)
	}
	p := f.materializeV128(f.popValue())
	f.pinned = f.pinned.union(p.mask())
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	f.pinned = f.pinned.add(ea)
	x := f.allocReg(p.mask().add(ea))
	f.pinned = f.pinned.remove(ea).remove(p.lo).remove(p.hi)
	f.a.LoadIdx(x, linMemReg, ea, disp, size, false, size == 8)
	f.replaceLaneFromReg(p, int(lane), size*8, x)
	f.release(x)
	if eaOwned {
		f.release(ea)
	}
	f.pushV128Pair(p)
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
	if int(lane) >= 16/size {
		return fmt.Errorf("riscv64: invalid v128.store%d_lane lane %d", size*8, lane)
	}
	f.materializePendingLoads()
	p := f.materializeV128(f.popValue())
	f.pinned = f.pinned.union(p.mask())
	x := f.allocReg(p.mask())
	f.extractLaneTo(x, p, int(lane), size*8, false)
	f.pinned = f.pinned.add(x)
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	f.a.StoreIdx(linMemReg, ea, x, disp, size)
	f.pinned = f.pinned.remove(x).remove(p.lo).remove(p.hi)
	if eaOwned {
		f.release(ea)
	}
	f.release(x)
	f.releaseV128(p)
	return nil
}

// swarSIMDSubopcodeValid is the RV64 lowering registry. The ratified core and
// relaxed proposal tables occupy 0..275 except for these 20 reserved holes.
func swarSIMDSubopcodeValid(sub uint32) bool {
	if sub > 275 {
		return false
	}
	switch sub {
	case 154, 162, 165, 166, 175, 176, 178, 179, 180, 187,
		194, 197, 198, 207, 208, 210, 211, 212, 226, 238:
		return false
	default:
		return true
	}
}

func (f *fn) emitFD(r *wasm.Reader) error {
	sub, err := r.U32()
	if err != nil {
		return err
	}
	if !swarSIMDSubopcodeValid(sub) {
		return fmt.Errorf("riscv64: SWAR SIMD opcode %d is not implemented", sub)
	}
	switch sub {
	case 0:
		return f.v128Load(r)
	case 1, 2, 3, 4, 5, 6:
		return f.v128LoadExtend(r, sub)
	case 7, 8, 9, 10:
		return f.v128LoadSplat(r, sub)
	case 11:
		return f.v128Store(r)
	case 84, 85, 86, 87:
		return f.v128LoadLane(r, sub)
	case 88, 89, 90, 91:
		return f.v128StoreLane(r, sub)
	case 92, 93:
		return f.v128LoadZero(r, sub)
	case 12: // v128.const
		var b [16]byte
		for i := range b {
			b[i], err = r.Byte()
			if err != nil {
				return err
			}
		}
		f.v128Const(binary.LittleEndian.Uint64(b[:8]), binary.LittleEndian.Uint64(b[8:]))
	case 13: // i8x16.shuffle
		var lanes [16]byte
		for i := range lanes {
			lane, err := r.Byte()
			if err != nil {
				return err
			}
			if lane >= 32 {
				return fmt.Errorf("riscv64: invalid i8x16.shuffle lane %d", lane)
			}
			lanes[i] = lane
		}
		f.i8x16Shuffle(lanes)
	case 14, 256: // strict swizzle is a deterministic relaxed-swizzle projection
		f.i8x16Swizzle()
	case 15: // i8x16.splat
		f.integerSplat(8)
	case 16: // i16x8.splat
		f.integerSplat(16)
	case 17: // i32x4.splat
		f.integerSplat(32)
	case 18: // i64x2.splat
		f.i64x2Splat()
	case 19:
		f.floatSplat(false)
	case 20:
		f.floatSplat(true)
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
	case 31, 33:
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.floatExtractLane(sub == 33, lane)
	case 32, 34:
		lane, err := r.Byte()
		if err != nil {
			return err
		}
		return f.floatReplaceLane(sub == 34, lane)
	case 35, 36, 37, 38, 39, 40, 41, 42, 43, 44:
		cond, signed := integerCompareSpec(sub, 35)
		f.integerCompare(8, cond, signed)
	case 45, 46, 47, 48, 49, 50, 51, 52, 53, 54:
		cond, signed := integerCompareSpec(sub, 45)
		f.integerCompare(16, cond, signed)
	case 55, 56, 57, 58, 59, 60, 61, 62, 63, 64:
		cond, signed := integerCompareSpec(sub, 55)
		f.integerCompare(32, cond, signed)
	case 65, 66, 67, 68, 69, 70:
		conds := [...]Cond{condE, condNE, condL, condG, condLE, condGE}
		f.floatCompare(false, conds[sub-65])
	case 71, 72, 73, 74, 75, 76:
		conds := [...]Cond{condE, condNE, condL, condG, condLE, condGE}
		f.floatCompare(true, conds[sub-71])
	case 77:
		f.v128Not()
	case 78, 79, 80, 81:
		f.v128Binary(sub)
	case 82:
		f.v128Bitselect()
	case 83:
		f.v128AnyTrue()
	case 94:
		f.floatDemotePromote(false)
	case 95:
		f.floatDemotePromote(true)
	case 96, 97: // i8x16.abs/neg
		f.integerUnary(8, sub == 96)
	case 98:
		f.i8x16Popcnt()
	case 99:
		f.integerAllTrue(8)
	case 100:
		f.integerBitmask(8)
	case 101, 102:
		f.integerNarrow(16, sub == 101)
	case 103, 104, 105, 106:
		f.floatUnary(false, sub)
	case 107, 108, 109: // i8x16 shifts
		f.integerShift(8, sub == 108, sub == 107)
	case 110, 113: // i8x16 add/sub
		f.packedAddSub(8, sub == 113)
	case 111, 112, 114, 115: // i8x16 saturating add/sub
		f.integerSaturating(8, sub == 111 || sub == 114, sub >= 114)
	case 116, 117:
		f.floatUnary(true, sub)
	case 118, 119, 120, 121: // i8x16 min/max
		f.integerMinMax(8, sub == 118 || sub == 120, sub >= 120)
	case 122:
		f.floatUnary(true, sub)
	case 123:
		f.integerAvgrU(8)
	case 124, 125:
		f.integerExtaddPairwise(8, sub == 124)
	case 126, 127:
		f.integerExtaddPairwise(16, sub == 126)
	case 128, 129: // i16x8.abs/neg
		f.integerUnary(16, sub == 128)
	case 130:
		f.i16x8Q15mulrSatS()
	case 131:
		f.integerAllTrue(16)
	case 132:
		f.integerBitmask(16)
	case 133, 134:
		f.integerNarrow(32, sub == 133)
	case 135, 136, 137, 138:
		f.integerExtend(8, sub == 135 || sub == 136, sub == 136 || sub == 138)
	case 139, 140, 141: // i16x8 shifts
		f.integerShift(16, sub == 140, sub == 139)
	case 142, 145: // i16x8 add/sub
		f.packedAddSub(16, sub == 145)
	case 143, 144, 146, 147: // i16x8 saturating add/sub
		f.integerSaturating(16, sub == 143 || sub == 146, sub >= 146)
	case 148:
		f.floatUnary(true, sub)
	case 149:
		f.integerMul(16)
	case 150, 151, 152, 153: // i16x8 min/max
		f.integerMinMax(16, sub == 150 || sub == 152, sub >= 152)
	case 155:
		f.integerAvgrU(16)
	case 156, 157, 158, 159:
		f.integerExtmul(8, sub == 156 || sub == 157, sub == 157 || sub == 159)
	case 160, 161: // i32x4.abs/neg
		f.integerUnary(32, sub == 160)
	case 163:
		f.integerAllTrue(32)
	case 164:
		f.integerBitmask(32)
	case 167, 168, 169, 170:
		f.integerExtend(16, sub == 167 || sub == 168, sub == 168 || sub == 170)
	case 171, 172, 173: // i32x4 shifts
		f.integerShift(32, sub == 172, sub == 171)
	case 174, 177: // i32x4 add/sub
		f.packedAddSub(32, sub == 177)
	case 181:
		f.integerMul(32)
	case 182, 183, 184, 185: // i32x4 min/max
		f.integerMinMax(32, sub == 182 || sub == 184, sub >= 184)
	case 186:
		f.i32x4DotI16x8S()
	case 188, 189, 190, 191:
		f.integerExtmul(16, sub == 188 || sub == 189, sub == 189 || sub == 191)
	case 192, 193: // i64x2.abs/neg
		f.integerUnary(64, sub == 192)
	case 195:
		f.integerAllTrue(64)
	case 196:
		f.integerBitmask(64)
	case 199, 200, 201, 202:
		f.integerExtend(32, sub == 199 || sub == 200, sub == 200 || sub == 202)
	case 203, 204, 205: // i64x2 shifts
		f.integerShift(64, sub == 204, sub == 203)
	case 206, 209, 213:
		f.i64x2Binary(sub)
	case 214:
		f.integerCompare(64, condE, false)
	case 215:
		f.integerCompare(64, condNE, false)
	case 216:
		f.integerCompare(64, condL, true)
	case 217:
		f.integerCompare(64, condG, true)
	case 218:
		f.integerCompare(64, condLE, true)
	case 219:
		f.integerCompare(64, condGE, true)
	case 220, 221, 222, 223:
		f.integerExtmul(32, sub == 220 || sub == 221, sub == 221 || sub == 223)
	case 224, 225, 227:
		f.floatUnary(false, sub)
	case 228, 229, 230, 231, 232, 233:
		f.floatBinary(false, sub)
	case 234, 235:
		f.floatPMinMax(false, sub == 235)
	case 236, 237, 239:
		f.floatUnary(true, sub)
	case 240, 241, 242, 243, 244, 245:
		f.floatBinary(true, sub)
	case 246, 247:
		f.floatPMinMax(true, sub == 247)
	case 248, 257:
		f.floatTruncSat(false, true)
	case 249, 258:
		f.floatTruncSat(false, false)
	case 250:
		f.integerToFloat(false, true)
	case 251:
		f.integerToFloat(false, false)
	case 252, 259:
		f.floatTruncSat(true, true)
	case 253, 260:
		f.floatTruncSat(true, false)
	case 254:
		f.integerToFloat(true, true)
	case 255:
		f.integerToFloat(true, false)
	case 261, 262:
		f.relaxedFloatMadd(false, sub == 262)
	case 263, 264:
		f.relaxedFloatMadd(true, sub == 264)
	case 265, 266, 267, 268: // deterministic bitselect relaxed-laneselect projection
		f.v128Bitselect()
	case 269, 270:
		f.floatBinary(false, sub)
	case 271, 272:
		f.floatBinary(true, sub)
	case 273: // deterministic strict projection for relaxed q15 multiply
		f.i16x8Q15mulrSatS()
	case 274:
		f.relaxedDotI8x16I7x16S()
	case 275:
		f.relaxedDotI8x16I7x16AddS()
	default:
		panic(fmt.Sprintf("riscv64: SWAR SIMD registry/dispatch mismatch for opcode %d", sub))
	}
	return nil
}
