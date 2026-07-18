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
	case 18: // i64x2.splat
		f.i64x2Splat()
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
	case 206, 209, 213:
		f.i64x2Binary(sub)
	default:
		return fmt.Errorf("riscv64: SWAR SIMD opcode %d is not implemented", sub)
	}
	return nil
}
