//go:build amd64

package amd64

import (
	"bytes"
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
	case stConst:
		if e.st.typ == mtV128 && e.st.cval == 0 {
			x := f.allocFReg(0)
			f.a.VPxor(x, x, x)
			f.occupyF(e, x)
			return x
		}
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
		// Pinned v128 local: the live value is in its XMM register (the frame slot
		// may be stale). Copy into an owned scratch so a destructive op on the result
		// cannot corrupt the local — mirrors arm64 materializeV128 and the scalar
		// materializeF stLocalReg copy.
		x := f.allocFReg(0)
		f.a.VMovdqu(x, e.st.reg)
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

// v128ConstReg holds a v128.const value cached in a reserved XMM register.
type v128ConstReg struct {
	lo, hi uint64
	reg    Reg
}

// maxV128Consts bounds how many distinct repeated v128 constants get a reserved
// XMM register per function. Covers both wasm v128.const immediates and the fixed
// masks/tables that emulation lowerings (popcnt, swizzle, q15mulr) rebuild in a
// hot loop otherwise, so it is a little above the two an emulation typically needs.
const maxV128Consts = 4

// emulationConsts returns the fixed 128-bit constants a SIMD opcode's lowering
// materializes in-register. preloadV128Consts reserves them like wasm v128.const
// immediates so a hot loop builds them once at entry instead of every iteration.
// Keep in sync with the corresponding lowerings.
func emulationConsts(sub uint32) [][2]uint64 {
	switch sub {
	case 0x62: // i8x16.popcnt: low-nibble mask + nibble popcount LUT
		return [][2]uint64{
			{0x0f0f0f0f0f0f0f0f, 0x0f0f0f0f0f0f0f0f},
			{0x0302020102010100, 0x0403030203020201},
		}
	case 0x0e: // i8x16.swizzle: saturating-add bias (0x70) for out-of-range zeroing
		return [][2]uint64{
			{0x7070707070707070, 0x7070707070707070},
		}
	case 130: // i16x8.q15mulr_sat_s: INT16_MIN detect + INT16_MAX saturate
		return [][2]uint64{
			{0x8000800080008000, 0x8000800080008000},
			{0x7fff7fff7fff7fff, 0x7fff7fff7fff7fff},
		}
	case 251: // f32x4.convert_i32x4_u: low-16 mask + 65536.0f scale
		return [][2]uint64{
			{0x0000ffff0000ffff, 0x0000ffff0000ffff},
			{0x4780000047800000, 0x4780000047800000},
		}
	case 252: // i32x4.trunc_sat_f64x2_s_zero: INT32_MAX as f64 per lane
		return [][2]uint64{
			{0x41dfffffffc00000, 0x41dfffffffc00000},
		}
	case 253: // i32x4.trunc_sat_f64x2_u_zero: UINT32_MAX as f64 + 2^52 magic
		return [][2]uint64{
			{0x41efffffffe00000, 0x41efffffffe00000},
			{0x4330000000000000, 0x4330000000000000},
		}
	case 255: // f64x2.convert_low_i32x4_u: 2^52 magic bias
		return [][2]uint64{
			{0x4330000000000000, 0x4330000000000000},
		}
	}
	return nil
}

// v128ConstReg returns a fresh OWNED XMM register holding the 128-bit constant
// (lo,hi). A repeated const cached at entry (preloadV128Consts) is copied from its
// reserved register with one VMOVDQU instead of rebuilding the immediate.
func (f *fn) v128ConstReg(lo, hi uint64) Reg {
	x := f.allocFReg(0)
	if lo == 0 && hi == 0 {
		f.a.VPxor(x, x, x)
		return x
	}
	if c, ok := f.v128ConstCached(lo, hi); ok {
		f.a.VMovdqu(x, c)
		return x
	}
	if !v128ConstCacheEnabled {
		f.buildV128Const(x, lo, hi) // A/B fallback: rebuild the immediate in-register
		return x
	}
	// Load from the function's trailing rip-relative constant pool with a single
	// MOVDQU, instead of rebuilding the 128-bit immediate (3-4 ops). This is what
	// makes real SIMD kernels — which use many constant tables/masks that overflow
	// the reserved-register cache — competitive: one load per use, no register
	// reserved. Mirrors wazero's rodata constant loads.
	site := f.a.MovdquRipPlaceholder(x)
	f.recordV128Const(lo, hi, site)
	return x
}

// poolConst is one constant (4, 8, or 16 bytes) in the function's trailing pool
// and the disp32 field offsets of every rip-relative load that references it.
type poolConst struct {
	data  []byte
	sites []int
}

// recordV128Const registers a MOVDQU rip-load site for the 128-bit constant.
func (f *fn) recordV128Const(lo, hi uint64, site int) {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[0:8], lo)
	binary.LittleEndian.PutUint64(b[8:16], hi)
	f.recordConst(b[:], site)
}

// recordConst registers a rip-load site for a constant, deduplicating by bytes so
// each distinct constant occupies the pool once. The data is copied (callers pass
// a reused scratch buffer).
func (f *fn) recordConst(data []byte, site int) {
	for i := range f.v128Pool {
		if bytes.Equal(f.v128Pool[i].data, data) {
			f.v128Pool[i].sites = append(f.v128Pool[i].sites, site)
			return
		}
	}
	f.v128Pool = append(f.v128Pool, poolConst{data: append([]byte(nil), data...), sites: []int{site}})
}

// emitV128ConstPool lays the collected constants after the function code (never
// executed — reached only via rip-relative loads) and patches every load's disp32
// to its constant. Call once at function finalization, after all code.
func (f *fn) emitV128ConstPool() {
	if len(f.v128Pool) == 0 {
		return
	}
	for _, c := range f.v128Pool {
		off := f.a.Len()
		f.a.EmitBytes(c.data)
		for _, s := range c.sites {
			f.a.PatchRel32(s, off)
		}
	}
	f.v128Pool = f.v128Pool[:0]
}

func (f *fn) buildV128Const(x Reg, lo, hi uint64) {
	t := f.allocReg(0)
	f.a.MovImm64(t, lo)
	f.a.MovGprToXmm(x, t, true) // MOVQ zeroes the high 64 bits.
	if hi != 0 {
		f.a.MovImm64(t, hi)
		f.a.Pinsrq(x, t, 1)
	}
	f.release(t)
}

// v128ConstMask blocks the reserved const registers from the XMM allocator, like
// fconstMask for scalar-float constants.
func (f *fn) v128ConstMask() regMask {
	var m regMask
	for _, c := range f.vconsts {
		m = m.add(c.reg)
	}
	return m
}

// v128ConstCached returns the reserved register holding (lo,hi), if any.
func (f *fn) v128ConstCached(lo, hi uint64) (Reg, bool) {
	for _, c := range f.vconsts {
		if c.lo == lo && c.hi == hi {
			return c.reg, true
		}
	}
	return regNone, false
}

// pinnedV128LocalCount counts the v128 locals held in a dedicated XMM register for
// the whole function — the baseline XMM pressure that gates const reservation.
func (f *fn) pinnedV128LocalCount() int {
	n := 0
	for i := range f.locals {
		if i >= len(f.localType) {
			break
		}
		if f.locals[i].reg != regNone && f.localType[i] == mtV128 {
			n++
		}
	}
	return n
}

// preloadV128Consts scans the body for v128.const immediates used more than once
// and reserves an XMM register for each (up to maxV128Consts), materialized once at
// entry. Skipped for call-making functions (a call clobbers XMM). When 2+ v128
// locals are already pinned, wasm v128.const reservation is skipped (funneling a
// loop-carried const through one register serializes the loop, per bitselect), but
// emulation constants (read-only masks/tables, never loop-carried) are still
// reserved. Mirrors preloadFloatConsts / arm64 preloadV128Consts.
func (f *fn) preloadV128Consts(code []byte) {
	if f.usesCalls || !v128ConstCacheEnabled {
		return
	}
	highPressure := f.pinnedV128LocalCount() >= 2
	var cand [8]struct {
		lo, hi uint64
		n      int
		emul   bool
	}
	nCand := 0
	addCand := func(lo, hi uint64, emul bool) {
		for i := 0; i < nCand; i++ {
			if cand[i].lo == lo && cand[i].hi == hi {
				cand[i].n++
				cand[i].emul = cand[i].emul || emul
				return
			}
		}
		if nCand < len(cand) {
			cand[nCand].lo, cand[nCand].hi = lo, hi
			cand[nCand].n, cand[nCand].emul = 1, emul
			nCand++
		}
	}
	r := wasm.NewReader(code)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return
		}
		if op != 0xFD { // not the SIMD prefix
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
		if sub == 12 { // v128.const: 16 immediate bytes follow
			lo, err := r.LEU64()
			if err != nil {
				return
			}
			hi, err := r.LEU64()
			if err != nil {
				return
			}
			addCand(lo, hi, false)
			continue
		}
		for _, c := range emulationConsts(sub) {
			addCand(c[0], c[1], true)
		}
		if err := r.JumpTo(afterPrefix); err != nil {
			return
		}
		if err := wasm.SkipInstructionImmediate(r, op); err != nil {
			return
		}
	}
	// Reserve emulation constants first (always beneficial, non-serializing), then
	// wasm v128.const immediates unless v128-local pressure is high. Unlike arm64
	// (which requires a static reuse count >= 2), reserve for any distinct non-zero
	// const: a const used once statically but inside a loop — the isa_simd reductions
	// — is rebuilt every iteration otherwise, and the 128-bit build on amd64 is 3
	// instructions. Bounded to maxV128Consts regs, so a rare straight-line single-use
	// const costs at most one extra copy.
	reserve := func(wantEmul bool) {
		for i := 0; i < nCand && len(f.vconsts) < maxV128Consts; i++ {
			if cand[i].emul != wantEmul {
				continue
			}
			if cand[i].lo == 0 && cand[i].hi == 0 {
				continue // the zero const is already a single VPXOR
			}
			x := f.allocFReg(0)
			f.buildV128Const(x, cand[i].lo, cand[i].hi)
			f.vconsts = append(f.vconsts, v128ConstReg{lo: cand[i].lo, hi: cand[i].hi, reg: x})
		}
	}
	reserve(true)
	if !highPressure {
		reserve(false)
	}
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

func (f *fn) v128FloatRound(f64 bool, mode byte) {
	a := f.popValue()
	x := f.materializeV128(a)
	f.a.VFRoundPacked(x, x, f64, mode)
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

func v128MaskBits(b [16]byte) (uint64, uint64) {
	return binary.LittleEndian.Uint64(b[0:8]), binary.LittleEndian.Uint64(b[8:16])
}

func (f *fn) i8x16Swizzle() {
	idxElem := f.popValue()
	srcElem := f.popValue()
	idx := f.materializeV128(idxElem)
	f.fpinned = f.fpinned.add(idx)
	src := f.materializeV128(srcElem)
	f.fpinned = f.fpinned.add(src)

	// PSHUFB zeros a lane only when its control byte has bit 7 set, and selects
	// with bits [3:0]. Wasm core swizzle zeros every unsigned byte index >= 16.
	// Saturating-add 0x70 to the index: 0..15 map to 0x70..0x7f (bit 7 clear,
	// low nibble preserved -> src[idx]); any index >= 16 reaches >= 0x80 (bit 7
	// set -> zero). One instruction replaces the xor/cmpgtb/and/or mask build.
	bias := f.v128ConstReg(0x7070707070707070, 0x7070707070707070)
	f.a.VPaddusb(idx, idx, bias)
	f.releaseF(bias)

	f.a.VPshufb(src, src, idx)
	f.fpinned = f.fpinned.remove(idx).remove(src)
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

	f.a.VPshufb(xa, xa, ma)
	f.fpinned = f.fpinned.remove(ma)
	f.releaseF(ma)
	f.a.VPshufb(xb, xb, mb)
	f.releaseF(mb)
	f.fpinned = f.fpinned.remove(xa).remove(xb)
	f.a.VPor(xa, xa, xb)
	f.releaseF(xb)
	f.pushVReg(xa)
}

// operandRegV128 returns the register holding e for read-only use by a 3-operand
// VEX op. A pinned v128 local is used in place (no copy, not owned); anything else
// is materialized into an owned scratch.
func (f *fn) operandRegV128(e *elem) (Reg, bool) {
	if e.kind == ekValue && e.st.kind == stLocalReg {
		return e.st.reg, false
	}
	return f.materializeV128(e), true
}

// v128Bin lowers a two-operand v128 op. When immediately consumed by
// `local.set/tee $x` into a pinned v128 local, tryV128BinLocalSet emits it in
// place into $x's XMM register (one instruction, no accumulator copy, no
// result-to-pin move). Otherwise it copies the left operand (the op writes it) and
// reads the right in place when it is a pinned local.
func (f *fn) v128Bin(r *wasm.Reader, op func(dst, s1, s2 Reg)) {
	if f.tryV128BinLocalSet(r, op) {
		return
	}
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a) // owned writable copy: op writes s1
	f.fpinned = f.fpinned.add(xa)
	xb, bOwned := f.operandRegV128(b)
	f.fpinned = f.fpinned.remove(xa)
	op(xa, xa, xb)
	if bOwned {
		f.releaseF(xb)
	}
	f.pushVReg(xa)
}

// v128BinInto emits op(dst, s1, s2) reading BOTH operands in place — dst is a
// pinned v128 local's register the result sinks into. A 3-operand VEX op reads
// both sources before writing dst, so any aliasing among dst/s1/s2 (the
// accumulator `x = x op y`, or `x = x op x`) is correct.
func (f *fn) v128BinInto(dst Reg, op func(dst, s1, s2 Reg)) {
	b := f.popValue()
	a := f.popValue()
	s1, o1 := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(s1)
	s2, o2 := f.operandRegV128(b)
	f.fpinned = f.fpinned.remove(s1)
	op(dst, s1, s2)
	if o1 && dst != s1 {
		f.releaseF(s1)
	}
	if o2 && dst != s2 {
		f.releaseF(s2)
	}
}

// tryV128BinLocalSet peeps `local.set/tee $x (v128bin A B)` where $x is a
// register-pinned v128 local and sinks the op straight into $x's register. Returns
// true when it fired (and consumed the local.set/tee); restores the reader and
// returns false on any mismatch. Reader errors during lookahead fall back to the
// eager path (the outer emitFD loop re-reads and surfaces them).
func (f *fn) tryV128BinLocalSet(r *wasm.Reader, op func(dst, s1, s2 Reg)) bool {
	if !v128LocalSinkEnabled {
		return false
	}
	save := r.Offset()
	nb, ok := r.Peek()
	if !ok || (nb != 0x21 && nb != 0x22) { // local.set / local.tee
		return false
	}
	if _, err := r.Byte(); err != nil {
		_ = r.JumpTo(save)
		return false
	}
	x32, err := r.U32()
	if err != nil {
		_ = r.JumpTo(save)
		return false
	}
	x := int(x32) + f.localBase
	pr, _, pinned := f.pinReg(x)
	if !pinned || x < 0 || x >= len(f.localType) || f.localType[x] != mtV128 {
		_ = r.JumpTo(save)
		return false
	}
	if f.bcKind == 1 && f.bcIdx == uint32(x) {
		f.invalidateBoundsCert()
	}
	right := f.s.back()
	if right == nil {
		_ = r.JumpTo(save)
		return false
	}
	// Realize refs to $x below the two operand blocks; the operands themselves are
	// consumed in place by v128BinInto.
	left := baseOfValentBlock(right).prev
	f.realizeLocalRefs(x, left)
	f.v128BinInto(pr, op)
	f.markLocalDirty(x)
	f.stats.peep("v128-local-sink")
	if nb == 0x22 { // local.tee keeps the value on the stack
		f.pushValue(storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: x})
	}
	return true
}

// v128FloatMinMax lowers f32x4/f64x2 min/max with the branchless vectorized
// sequence that reproduces Wasm's NaN and signed-zero semantics: two commuted
// MINPS/MAXPS results are combined (OR for min so −0 wins, AND for max so +0
// wins), NaN lanes are forced on via an unordered compare, then canonicalized by
// clearing the low mantissa bits. This replaces a per-lane scalar extract/insert
// loop (~30 instructions) with ~7 vector instructions.
func (f *fn) v128FloatMinMax(f64, isMax bool) {
	bElem := f.popValue()
	aElem := f.popValue()
	xa := f.materializeV128(aElem)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(bElem)
	f.fpinned = f.fpinned.add(xb)

	pp := byte(0) // ps
	if f64 {
		pp = 1 // pd
	}
	packed := f.a.VFPackedMin
	if isMax {
		packed = f.a.VFPackedMax
	}

	t := f.allocFReg(maskOf(xa, xb))
	f.fpinned = f.fpinned.add(t)
	c := f.allocFReg(maskOf(xa, xb, t))

	packed(t, xa, xb, f64)  // t  = op(a, b)
	packed(xa, xb, xa, f64) // xa = op(b, a); commuted copy differs only for ±0/NaN

	f.a.VFCmpPacked(c, t, xa, f64, 0x03) // c = unordered mask (all-ones in NaN lanes)
	if isMax {
		f.a.VSseRRR(pp, 0x54, t, t, xa) // andps: +0 beats −0 for max
	} else {
		f.a.VSseRRR(pp, 0x56, t, t, xa) // orps: −0 beats +0 for min
	}
	f.a.VSseRRR(pp, 0x56, t, t, c) // orps: force NaN lanes to all-ones
	if f64 {
		f.a.VPsrlqImm(c, c, 13) // low 51 bits: mantissa minus its MSB
	} else {
		f.a.VPsrldImm(c, c, 10) // low 22 bits: mantissa minus its MSB
	}
	f.a.VSseRRR(pp, 0x55, t, c, t) // andnps: clear those bits → canonical NaN, others unchanged

	f.releaseF(c)
	f.fpinned = f.fpinned.remove(xa).remove(xb).remove(t)
	f.releaseF(xa)
	f.releaseF(xb)
	f.pushVReg(t)
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
	f.a.VPand(xa, xa, mask)
	f.a.VPandn(xb, mask, xb)
	f.a.VPor(xa, xa, xb)
	f.fpinned = f.fpinned.remove(mask).remove(xb)
	f.releaseF(mask)
	f.releaseF(xb)
	f.pushVReg(xa)
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

	f.a.VFPackedMul(xa, xa, xb, f64)
	f.fpinned = f.fpinned.remove(xb)
	f.releaseF(xb)
	if neg {
		f.a.VFPackedSub(xc, xc, xa, f64) // relaxed_nmadd: c - (a * b), without FMA.
		f.fpinned = f.fpinned.remove(xa)
		f.releaseF(xa)
		f.pushVReg(xc)
		return
	}
	f.a.VFPackedAdd(xa, xa, xc, f64)
	f.releaseF(xc)
	f.fpinned = f.fpinned.remove(xa)
	f.pushVReg(xa)
}

// TODO(simd): Vectorize signed packed trunc_sat with SSE/AVX where practical;
// keep unsigned and saturating scalar fallback as the correctness baseline.
// v128I32x4TruncSat lowers the saturating float->i32 conversions. The f32x4
// forms use a fully vectorized branchless sequence (wazero/V8-proven); the
// f64x2 *_zero forms still use the per-lane scalar fallback below.
func (f *fn) v128I32x4TruncSat(f64src, signed bool) {
	switch {
	case !f64src:
		f.v128TruncSatF32x4(signed)
	case signed:
		f.v128TruncSatF64x2SignedZero()
	default:
		f.v128TruncSatF64x2UnsignedZero()
	}
}

// v128TruncSatF64x2UnsignedZero lowers i32x4.trunc_sat_f64x2_u_zero: clamp to
// [0, UINT32_MAX], round toward zero, then extract the low 32 bits via the 2^52
// magic bias and a SHUFPS that packs lanes 0,2 and zeroes the upper half.
// Branchless; mirrors wazero's f64x2 unsigned path.
func (f *fn) v128TruncSatF64x2UnsignedZero() {
	xx := f.materializeV128(f.popValue())
	f.fpinned = f.fpinned.add(xx)
	zero := f.allocFReg(maskOf(xx))
	f.fpinned = f.fpinned.add(zero)
	f.a.VPxor(zero, zero, zero)
	f.a.VSseRRR(1, 0x5f, xx, xx, zero) // MAXPD: clamp negatives/NaN to 0
	maxc := f.v128ConstReg(0x41efffffffe00000, 0x41efffffffe00000)
	f.a.VSseRRR(1, 0x5d, xx, xx, maxc) // MINPD: clamp to UINT32_MAX
	f.releaseF(maxc)
	f.a.VFRoundPacked(xx, xx, true, roundTrunc) // truncate toward zero
	magic := f.v128ConstReg(0x4330000000000000, 0x4330000000000000)
	f.a.VSseRRR(1, 0x58, xx, xx, magic) // ADDPD: 2^52 + uint32(vi) in low 32 bits
	f.releaseF(magic)
	f.a.VShufps(xx, xx, zero, 0b00_00_10_00) // pack lanes 0,2 -> dwords 0,1; upper = 0
	f.fpinned = f.fpinned.remove(zero)
	f.releaseF(zero)
	f.fpinned = f.fpinned.remove(xx)
	f.pushVReg(xx)
}

// v128TruncSatF64x2SignedZero lowers i32x4.trunc_sat_f64x2_s_zero: clamp NaN to 0
// and positive overflow to INT_MAX via MINPD against 2147483647.0, then narrow
// with CVTTPD2DQ (which handles negative overflow and zeroes the upper 2 lanes).
// Branchless; mirrors wazero's f64x2 signed path.
func (f *fn) v128TruncSatF64x2SignedZero() {
	xx := f.materializeV128(f.popValue())
	f.fpinned = f.fpinned.add(xx)
	tmp := f.allocFReg(maskOf(xx))
	f.fpinned = f.fpinned.add(tmp)
	f.a.VMovdqu(tmp, xx)
	f.a.VFCmpPacked(tmp, tmp, tmp, true, vfcmpEqOQ) // non-NaN mask
	maxc := f.v128ConstReg(0x41dfffffffc00000, 0x41dfffffffc00000)
	f.a.VSseRRR(0, 0x54, tmp, tmp, maxc) // ANDPS: 2147483647.0 where non-NaN, else 0
	f.releaseF(maxc)
	f.a.VSseRRR(1, 0x5d, xx, xx, tmp) // MINPD: clamp +overflow; NaN lane -> 0
	f.a.Vcvttpd2dq(xx, xx)            // narrow to i32 low lanes, upper zeroed
	f.fpinned = f.fpinned.remove(tmp)
	f.releaseF(tmp)
	f.fpinned = f.fpinned.remove(xx)
	f.pushVReg(xx)
}

// v128TruncSatF32x4 lowers i32x4.trunc_sat_f32x4_{s,u} with no branches and no
// per-lane extract/insert. CVTTPS2DQ yields 0x80000000 for NaN and out-of-range
// lanes; the surrounding mask arithmetic patches NaN->0 and positive-overflow->
// INT_MAX (signed) / clamps to [0, UINT32_MAX] (unsigned). Mirrors wazero's
// lowerVFcvtToIntSat, translated to 3-operand VEX.
func (f *fn) v128TruncSatF32x4(signed bool) {
	xx := f.materializeV128(f.popValue()) // owned; becomes the result
	f.fpinned = f.fpinned.add(xx)
	tmp := f.allocFReg(maskOf(xx))
	f.fpinned = f.fpinned.add(tmp)
	if signed {
		f.a.VMovdqu(tmp, xx)
		f.a.VFCmpPacked(tmp, tmp, tmp, false, vfcmpEqOQ) // tmp = non-NaN mask
		f.a.VSseRRR(0, 0x54, xx, xx, tmp)                // ANDPS: NaN lanes -> +0.0
		f.a.VSseRRR(0, 0x57, tmp, tmp, xx)               // XORPS: tmp sign bit set iff lane negative
		f.a.Vcvttps2dq(xx, xx)                           // trunc; 0x80000000 on NaN/overflow
		f.a.VSseRRR(0, 0x54, tmp, tmp, xx)               // ANDPS
		f.a.VPsradImm(tmp, tmp, 31)                      // all-ones where positive overflow
		f.a.VPxor(xx, xx, tmp)                           // 0x80000000 -> 0x7FFFFFFF for +overflow
	} else {
		zero := f.allocFReg(maskOf(xx, tmp))
		f.fpinned = f.fpinned.add(zero)
		tmp2 := f.allocFReg(maskOf(xx, tmp, zero))
		f.a.VPxor(zero, zero, zero)
		f.a.VSseRRR(0, 0x5F, xx, xx, zero) // MAXPS: clamp negatives and NaN to 0
		f.a.VPcmpeqd(tmp, tmp, tmp)
		f.a.VPsrldImm(tmp, tmp, 1)            // 0x7FFFFFFF
		f.a.Vcvtdq2ps(tmp, tmp)               // 2147483647.0f
		f.a.VMovdqu(tmp2, xx)                 // tmp2 = clamped value
		f.a.Vcvttps2dq(xx, xx)                // low half: trunc of the clamped signed range
		f.a.VSseRRR(0, 0x5C, tmp2, tmp2, tmp) // SUBPS: tmp2 -= 2^31f
		f.a.VFCmpPacked(tmp, tmp, tmp2, false, vfcmpLeOQ)
		f.a.Vcvttps2dq(tmp2, tmp2)
		f.a.VPxor(tmp2, tmp2, tmp)
		f.a.VPxor(tmp, tmp, tmp)
		f.a.VPmaxsd(tmp2, tmp2, tmp)
		f.a.VPaddd(xx, xx, tmp2) // recombine the two halves
		f.fpinned = f.fpinned.remove(zero)
		f.releaseF(zero)
		f.releaseF(tmp2)
	}
	f.fpinned = f.fpinned.remove(tmp)
	f.releaseF(tmp)
	f.fpinned = f.fpinned.remove(xx)
	f.pushVReg(xx)
}

// v128DemoteF64x2Zero lowers f32x4.demote_f64x2_zero to one VCVTPD2PS: it writes
// the two converted floats to the low 64 bits and zeroes the upper two lanes,
// exactly the Wasm semantics. Same x86 conversion (rounding, NaN quieting) as the
// prior per-lane CVTSD2SS loop.
func (f *fn) v128DemoteF64x2Zero() {
	src, owned := f.operandRegV128(f.popValue())
	out := f.allocFReg(maskOf(src))
	f.a.Vcvtpd2ps(out, src)
	if owned {
		f.releaseF(src)
	}
	f.pushVReg(out)
}

// v128PromoteLowF32x4 lowers f64x2.promote_low_f32x4 to one VCVTPS2PD (low two
// f32 lanes -> two f64).
func (f *fn) v128PromoteLowF32x4() {
	src, owned := f.operandRegV128(f.popValue())
	out := f.allocFReg(maskOf(src))
	f.a.Vcvtps2pd(out, src)
	if owned {
		f.releaseF(src)
	}
	f.pushVReg(out)
}

// v128I32x4ConvertToFloat lowers int->float conversions. Signed forms use one
// packed instruction (VCVTDQ2PS for f32x4, VCVTDQ2PD for the low two lanes to
// f64x2). The unsigned forms have no single pre-AVX512 packed op, so they use
// exact vector tricks: split-16 (hi*65536 + lo, rounded once) for u32->f32 and
// the 2^52 magic bias for u32->f64.
func (f *fn) v128I32x4ConvertToFloat(f64dst, signed bool) {
	if signed {
		src, owned := f.operandRegV128(f.popValue())
		out := f.allocFReg(maskOf(src))
		if f64dst {
			f.a.Vcvtdq2pd(out, src)
		} else {
			f.a.Vcvtdq2ps(out, src)
		}
		if owned {
			f.releaseF(src)
		}
		f.pushVReg(out)
		return
	}
	srcElem := f.popValue()
	src := f.materializeV128(srcElem)
	f.fpinned = f.fpinned.add(src)

	if f64dst {
		// u32 -> f64 (low 2 lanes) is exact: zero-extend each u32 into a qword,
		// OR in 2^52, then subtract 2^52. (2^52 | u) - 2^52 == u with one exact
		// f64 result since u < 2^32 < 2^53.
		zero := f.allocFReg(maskOf(src))
		f.fpinned = f.fpinned.add(zero)
		f.a.VPxor(zero, zero, zero)
		zx := f.allocFReg(maskOf(src, zero))
		f.fpinned = f.fpinned.add(zx)
		f.a.VPunpckldq(zx, src, zero) // [u0,0,u1,0] -> qwords {u0,u1}
		f.fpinned = f.fpinned.remove(zero)
		f.releaseF(zero)
		magic := f.v128ConstReg(0x4330000000000000, 0x4330000000000000)
		f.a.VPor(zx, zx, magic)
		f.a.VFPackedSub(zx, zx, magic, true)
		f.releaseF(magic)
		f.fpinned = f.fpinned.remove(zx)
		f.fpinned = f.fpinned.remove(src)
		f.releaseF(src)
		f.pushVReg(zx)
		return
	}

	// u32 -> f32: split each lane into low/high 16-bit halves. Both are in
	// [0,65535], exactly representable in f32, so convert each with the signed
	// CVTDQ2PS and recombine as hi*65536 + lo (hi*65536 is an exact power-of-two
	// scale, so the final add rounds once).
	mask := f.v128ConstReg(0x0000ffff0000ffff, 0x0000ffff0000ffff)
	f.fpinned = f.fpinned.add(mask)
	lo := f.allocFReg(maskOf(src, mask))
	f.fpinned = f.fpinned.add(lo)
	f.a.VPand(lo, src, mask)
	f.fpinned = f.fpinned.remove(mask)
	f.releaseF(mask)
	hi := f.allocFReg(maskOf(src, lo))
	f.fpinned = f.fpinned.add(hi)
	f.a.VPsrldImm(hi, src, 16)
	f.a.Vcvtdq2ps(lo, lo)
	f.a.Vcvtdq2ps(hi, hi)
	scale := f.v128ConstReg(0x4780000047800000, 0x4780000047800000) // 65536.0f
	f.a.VFPackedMul(hi, hi, scale, false)
	f.releaseF(scale)
	f.a.VFPackedAdd(lo, lo, hi, false)
	f.fpinned = f.fpinned.remove(hi)
	f.releaseF(hi)
	f.fpinned = f.fpinned.remove(lo)
	f.fpinned = f.fpinned.remove(src)
	f.releaseF(src)
	f.pushVReg(lo)
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

// i8x16 shift kinds, used to pick the constant-count fast path.
const (
	i8ShiftShl  = 0
	i8ShiftShrS = 1
	i8ShiftShrU = 2
)

// bcastByte replicates a byte across all 8 bytes of a uint64.
func bcastByte(b byte) uint64 { return uint64(b) * 0x0101010101010101 }

// i8x16Shift lowers i8x16.{shl,shr_s,shr_u}. A compile-time-constant count takes
// a fast path (immediate word shift + a constant byte mask — the shape real SIMD
// code like UTF-8 nibble extraction uses); a runtime count falls back to the
// general widen/shift/pack sequence below.
func (f *fn) i8x16Shift(op func(dst, s1, s2 Reg), kind int) {
	signed := kind == i8ShiftShrS
	countElem := f.popValue()
	if countElem.kind == ekValue && countElem.st.kind == stConst {
		f.i8x16ShiftConst(kind, byte(countElem.st.cval&7))
		return
	}
	count := f.materialize(countElem)
	f.a.AluRI(4, count, 7, false) // Wasm shifts use count modulo 8 for i8 lanes.

	value := f.popValue()
	x := f.materializeV128(value)
	f.fpinned = f.fpinned.add(x)
	countX := f.allocFReg(maskOf(x))
	f.fpinned = f.fpinned.add(countX)
	f.a.MovGprToXmm(countX, count, false)
	f.release(count)

	hi := f.allocFReg(0)
	f.a.VPor(hi, x, x)
	if signed {
		f.a.VPunpcklbw(x, x, x)
		f.a.VPunpckhbw(hi, hi, hi)
		f.a.VPsrawImm(x, x, 8)
		f.a.VPsrawImm(hi, hi, 8)
	} else {
		z := f.allocFReg(maskOf(x, hi, countX))
		f.a.VPxor(z, z, z)
		f.a.VPunpcklbw(x, x, z)
		f.a.VPunpckhbw(hi, hi, z)
		f.releaseF(z)
	}

	op(x, x, countX)
	op(hi, hi, countX)
	f.fpinned = f.fpinned.remove(countX)
	f.releaseF(countX)

	if signed {
		f.a.VPpacksswb(x, x, hi)
	} else {
		mask := f.allocFReg(maskOf(x, hi))
		f.a.VPcmpeqw(mask, mask, mask) // 0xffff per word
		f.a.VPsrlwImm(mask, mask, 8)   // 0x00ff per word (in-register, no const load)
		f.a.VPand(x, x, mask)
		f.a.VPand(hi, hi, mask)
		f.releaseF(mask)
		f.a.VPpackuswb(x, x, hi)
	}
	f.releaseF(hi)
	f.fpinned = f.fpinned.remove(x)
	f.pushVReg(x)
}

// i8x16ShiftConst lowers a byte-lane shift by a compile-time constant n (0..7).
// x86 has no packed byte shift, but for a known n it is a 16-bit lane shift plus
// a constant byte mask that clears the bits that crossed lane boundaries — two to
// four ops with all-constant masks (cacheable), versus the runtime widen/pack.
func (f *fn) i8x16ShiftConst(kind int, n byte) {
	x := f.materializeV128(f.popValue())
	if n == 0 {
		f.pushVReg(x) // shift by 0 is the identity
		return
	}
	f.fpinned = f.fpinned.add(x)
	switch kind {
	case i8ShiftShl:
		f.a.VPsllwImm(x, x, n)
		m := f.v128ConstReg(bcastByte((0xff<<n)&0xff), bcastByte((0xff<<n)&0xff))
		f.a.VPand(x, x, m)
		f.releaseF(m)
	case i8ShiftShrU:
		f.a.VPsrlwImm(x, x, n)
		m := f.v128ConstReg(bcastByte(0xff>>n), bcastByte(0xff>>n))
		f.a.VPand(x, x, m)
		f.releaseF(m)
	default: // i8ShiftShrS: logical shift + mask, then (t ^ bias) - bias sign-extends
		f.a.VPsrlwImm(x, x, n)
		m := f.v128ConstReg(bcastByte(0xff>>n), bcastByte(0xff>>n))
		f.a.VPand(x, x, m)
		f.releaseF(m)
		bias := f.v128ConstReg(bcastByte(0x80>>n), bcastByte(0x80>>n))
		f.a.VPxor(x, x, bias)
		f.a.VPsubb(x, x, bias)
		f.releaseF(bias)
	}
	f.fpinned = f.fpinned.remove(x)
	f.pushVReg(x)
}

func (f *fn) i16x8Shift(op func(dst, s1, s2 Reg)) { f.v128Shift(op, 15) }

func (f *fn) i32x4Shift(op func(dst, s1, s2 Reg)) { f.v128Shift(op, 31) }

func (f *fn) i64x2Shift(op func(dst, s1, s2 Reg)) { f.v128Shift(op, 63) }

func (f *fn) i64x2ShrS() {
	countElem := f.popValue()
	count := f.materialize(countElem)
	f.a.AluRI(4, count, 63, false) // Wasm shifts use count modulo lane width.
	if count != RCX {
		f.spillIfUsed(RCX)
		f.a.MovReg64(RCX, count)
		f.release(count)
	}
	f.pinned = f.pinned.add(RCX)

	value := f.popValue()
	x := f.materializeV128(value)
	lo := f.allocReg(maskOf(RCX))
	f.pinned = f.pinned.add(lo)
	hi := f.allocReg(maskOf(RCX, lo))

	f.a.MovXmmToGpr(lo, x, true)
	f.a.Pextrq(hi, x, 1)
	f.a.ShiftCL(7, lo, true) // sar lo, cl
	f.a.ShiftCL(7, hi, true) // sar hi, cl
	f.a.MovGprToXmm(x, lo, true)
	f.a.Pinsrq(x, hi, 1)

	f.release(hi)
	f.pinned = f.pinned.remove(lo)
	f.release(lo)
	f.pinned = f.pinned.remove(RCX)
	f.release(RCX)
	f.pushVReg(x)
}

// i64x2Abs computes abs = (x ^ sign) - sign, where sign is the per-qword sign
// mask obtained as (0 > x) via VPCMPGTQ — no scalar lane extraction.
func (f *fn) i64x2Abs() {
	value := f.popValue()
	x := f.materializeV128(value)
	sign := f.allocFReg(maskOf(x))
	f.a.VPxor(sign, sign, sign) // zero
	f.a.VPcmpgtq(sign, sign, x) // sign = (0 > x) → all-ones per negative qword
	f.a.VPxor(x, x, sign)
	f.a.VPsubq(x, x, sign)
	f.releaseF(sign)
	f.pushVReg(x)
}

func (f *fn) i64x2Mul() {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)

	// 64-bit lane multiply without VPMULLQ (AVX-512): split each qword into
	// 32-bit halves and recompose r = aLo*bLo + ((aLo*bHi + aHi*bLo) << 32).
	// VPMULUDQ multiplies the low 32 bits of each qword lane (32x32->64), so
	// this stays fully in XMM and avoids the slow per-lane XMM<->GPR shuffles.
	cross := f.allocFReg(maskOf(xa, xb))
	f.fpinned = f.fpinned.add(cross)
	t := f.allocFReg(maskOf(xa, xb, cross))

	f.a.VPsrlqImm(cross, xb, 32)   // cross = bHi
	f.a.VPmuludq(cross, cross, xa) // cross = aLo * bHi
	f.a.VPsrlqImm(t, xa, 32)       // t = aHi
	f.a.VPmuludq(t, t, xb)         // t = aHi * bLo
	f.a.VPaddq(cross, cross, t)    // cross = aLo*bHi + aHi*bLo
	f.a.VPsllqImm(cross, cross, 32)
	f.a.VPmuludq(xa, xa, xb) // xa = aLo * bLo
	f.a.VPaddq(xa, xa, cross)

	f.releaseF(t)
	f.fpinned = f.fpinned.remove(cross)
	f.releaseF(cross)
	f.fpinned = f.fpinned.remove(xb)
	f.releaseF(xb)
	f.fpinned = f.fpinned.remove(xa)
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
	// VPMADDUBSW multiplies unsigned*signed byte pairs and adds adjacent
	// results into i16 lanes: with a vector of 1s it becomes a pairwise add.
	// Sums of two i8/u8 fit in i16, so no saturation occurs. Put the operand
	// carrying the value's signedness on the matching input.
	ones := f.allocFReg(maskOf(x))
	f.a.VPcmpeqb(ones, ones, ones) // 0xFF per byte
	f.a.VPabsb(ones, ones)         // 0x01 per byte
	if signed {
		f.a.VPmaddubsw(x, ones, x) // ones (unsigned) * x (signed)
	} else {
		f.a.VPmaddubsw(x, x, ones) // x (unsigned) * ones (signed)
	}
	f.releaseF(ones)
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
	if signed {
		// VPMADDWD with a vector of 1s is a signed pairwise 16->32 add.
		ones := f.allocFReg(maskOf(x))
		f.a.VPcmpeqw(ones, ones, ones) // 0xFFFF per word
		f.a.VPsrlwImm(ones, ones, 15)  // 0x0001 per word
		f.a.VPmaddwd(x, x, ones)
		f.releaseF(ones)
		f.pushVReg(x)
		return
	}
	hi := f.allocFReg(maskOf(x))
	f.a.VPor(hi, x, x)
	{
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

func (f *fn) relaxedDotI8x16I7x16PairSInto(dst, tmp, tmp2, xa, xb Reg, pair int, min, max Reg) {
	lane := byte(pair * 2)
	f.a.Pextrb(dst, xa, lane)
	f.a.Movsx8(dst, dst, false)
	f.a.Pextrb(tmp, xb, lane)
	f.a.Movsx8(tmp, tmp, false)
	f.a.IMul(dst, tmp, false)

	f.a.Pextrb(tmp, xa, lane+1)
	f.a.Movsx8(tmp, tmp, false)
	f.a.Pextrb(tmp2, xb, lane+1)
	f.a.Movsx8(tmp2, tmp2, false)
	f.a.IMul(tmp, tmp2, false)
	f.a.Add32(dst, tmp)

	// Deterministic relaxed choice: signed i8×signed i8 products with a signed
	// saturating i16 pair sum. This matches the portable Wasm relaxed-dot
	// semantics without requiring AVX2/VNNI dot-product instructions.
	f.a.Cmp32(dst, min)
	f.a.Cmovcc(condL, dst, min, false)
	f.a.Cmp32(dst, max)
	f.a.Cmovcc(condG, dst, max, false)
}

func (f *fn) relaxedDotI8x16I7x16Setup() (xa, xb, out, r0, r1, r2, r3, min, max Reg) {
	b := f.popValue()
	a := f.popValue()
	xa = f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb = f.materializeV128(b)
	f.fpinned = f.fpinned.add(xb)
	out = f.allocFReg(maskOf(xa, xb))
	f.fpinned = f.fpinned.add(out)
	f.a.VPxor(out, out, out)

	r0 = f.allocReg(0)
	f.pinned = f.pinned.add(r0)
	r1 = f.allocReg(maskOf(r0))
	f.pinned = f.pinned.add(r1)
	r2 = f.allocReg(maskOf(r0, r1))
	f.pinned = f.pinned.add(r2)
	r3 = f.allocReg(maskOf(r0, r1, r2))
	f.pinned = f.pinned.add(r3)
	min = f.allocReg(maskOf(r0, r1, r2, r3))
	f.pinned = f.pinned.add(min)
	max = f.allocReg(maskOf(r0, r1, r2, r3, min))
	f.a.MovImm64(min, uint64(uint32(0xffff8000)))
	f.a.MovImm64(max, 32767)
	return xa, xb, out, r0, r1, r2, r3, min, max
}

func (f *fn) relaxedDotI8x16I7x16Teardown(xa, xb, out, r0, r1, r2, r3, min, max Reg) {
	f.release(max)
	f.pinned = f.pinned.remove(min)
	f.release(min)
	f.pinned = f.pinned.remove(r3)
	f.release(r3)
	f.pinned = f.pinned.remove(r2)
	f.release(r2)
	f.pinned = f.pinned.remove(r1)
	f.release(r1)
	f.pinned = f.pinned.remove(r0)
	f.release(r0)
	f.fpinned = f.fpinned.remove(xa).remove(xb).remove(out)
	f.releaseF(xb)
	f.releaseF(xa)
}

func (f *fn) i16x8RelaxedDotI8x16I7x16S() {
	xa, xb, out, r0, r1, r2, r3, min, max := f.relaxedDotI8x16I7x16Setup()
	for pair := 0; pair < 8; pair++ {
		f.relaxedDotI8x16I7x16PairSInto(r0, r1, r2, xa, xb, pair, min, max)
		f.a.Pinsrw(out, r0, byte(pair))
	}
	f.relaxedDotI8x16I7x16Teardown(xa, xb, out, r0, r1, r2, r3, min, max)
	f.pushVReg(out)
}

func (f *fn) i32x4RelaxedDotI8x16I7x16AddS() {
	cElem := f.popValue()
	xc := f.materializeV128(cElem)
	f.fpinned = f.fpinned.add(xc)
	xa, xb, out, r0, r1, r2, r3, min, max := f.relaxedDotI8x16I7x16Setup()
	for lane := 0; lane < 4; lane++ {
		f.relaxedDotI8x16I7x16PairSInto(r0, r1, r2, xa, xb, lane*2, min, max)
		f.relaxedDotI8x16I7x16PairSInto(r1, r2, r3, xa, xb, lane*2+1, min, max)
		f.a.Add32(r0, r1)
		f.a.Pextrd(r1, xc, byte(lane))
		f.a.Add32(r0, r1)
		f.a.Pinsrd(out, r0, byte(lane))
	}
	f.relaxedDotI8x16I7x16Teardown(xa, xb, out, r0, r1, r2, r3, min, max)
	f.fpinned = f.fpinned.remove(xc)
	f.releaseF(xc)
	f.pushVReg(out)
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

// v128UnsignedCmp lowers the unsigned lane compares without the sign-bias
// constant the previous version rebuilt each call. It uses the unsigned min/max:
// ge_u(a,b) = (maxu(a,b) == a), le_u(a,b) = (minu(a,b) == a); gt_u/lt_u are those
// inverted. mmOp is VPMAXU* (ge/lt) or VPMINU* (le/gt), eqOp the lane-width
// VPCMPEQ, invert requests the NOT for the strict forms.
func (f *fn) v128UnsignedCmp(mmOp, eqOp func(dst, s1, s2 Reg), invert bool) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb, bOwned := f.operandRegV128(b)
	f.fpinned = f.fpinned.remove(xa)
	t := f.allocFReg(maskOf(xa, xb))
	mmOp(t, xa, xb) // min/max of the two
	eqOp(xa, t, xa) // xa = (min/max == a)
	f.releaseF(t)
	if bOwned {
		f.releaseF(xb)
	}
	if invert {
		m := f.allocFReg(maskOf(xa))
		f.a.VPcmpeqb(m, m, m)
		f.a.VPxor(xa, xa, m)
		f.releaseF(m)
	}
	f.pushVReg(xa)
}

// i64x2SignedCmp lowers the i64x2 signed compares via VPCMPGTQ (one instruction
// for gt_s/lt_s; a trailing bitwise-NOT for ge_s/le_s), replacing the per-lane
// scalar extract/Cmp64/setcc/reinsert emulation. cc is the wasm compare:
// condG=gt_s, condL=lt_s, condLE=le_s, condGE=ge_s.
func (f *fn) i64x2SignedCmp(cc Cond) {
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a) // op writes into xa
	f.fpinned = f.fpinned.add(xa)
	xb, bOwned := f.operandRegV128(b)
	f.fpinned = f.fpinned.remove(xa)

	// gt_s = gt(a,b); lt_s = gt(b,a); le_s = !gt(a,b); ge_s = !gt(b,a).
	swap := cc == condL || cc == condGE
	invert := cc == condLE || cc == condGE
	if swap {
		f.a.VPcmpgtq(xa, xb, xa)
	} else {
		f.a.VPcmpgtq(xa, xa, xb)
	}
	if invert {
		ones := f.allocFReg(maskOf(xa, xb))
		f.a.VPcmpeqd(ones, ones, ones) // all-ones (x == x per dword)
		f.a.VPxor(xa, xa, ones)
		f.releaseF(ones)
	}
	if bOwned {
		f.releaseF(xb)
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

func (f *fn) v128FCmp(r *wasm.Reader, f64 bool, pred byte) {
	f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFCmpPacked(dst, s1, s2, f64, pred) })
}

// v128FloatSignOp lowers f32x4/f64x2 abs (ANDPS, op 0x54) and neg (XORPS, op
// 0x57). The lane mask is built in-register — all-ones (VPCMPEQD) then an
// immediate shift — so there is no per-op 128-bit constant load (previously
// ~4-5 instructions from v128ConstReg). abs mask = 0x7fff… (all-ones >> 1);
// neg mask = 0x8000… (all-ones << lanebits-1).
func (f *fn) v128FloatSignOp(f64, isAbs bool, op byte) {
	v := f.popValue()
	x := f.materializeV128(v)
	f.fpinned = f.fpinned.add(x)
	mask := f.allocFReg(maskOf(x))
	f.a.VPcmpeqd(mask, mask, mask) // all-ones (x == x per dword)
	switch {
	case isAbs && f64:
		f.a.VPsrlqImm(mask, mask, 1)
	case isAbs:
		f.a.VPsrldImm(mask, mask, 1)
	case f64:
		f.a.VPsllqImm(mask, mask, 63)
	default:
		f.a.VPslldImm(mask, mask, 31)
	}
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

// tryV128AndAnyTrue selects `(a & b) != 0` before v128.and materializes its
// result. VPTEST sets ZF directly from the bitwise intersection, eliminating
// the temporary vector, zero vector, byte compare, movemask, and integer cmp.
func matchNextSIMDOp(r *wasm.Reader, want uint32) bool {
	save := r.Offset()
	prefix, err := r.Byte()
	if err != nil || prefix != 0xfd {
		_ = r.JumpTo(save)
		return false
	}
	sub, err := r.U32()
	if err != nil || sub != want {
		_ = r.JumpTo(save)
		return false
	}
	return true
}

func (f *fn) tryV128AndAnyTrue(r *wasm.Reader) bool {
	if !simdSuperoptEnabled || !matchNextSIMDOp(r, 83) {
		return false
	}
	b := f.popValue()
	a := f.popValue()
	sa, oa := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(sa)
	sb, ob := f.operandRegV128(b)
	f.fpinned = f.fpinned.remove(sa)
	f.a.VPtest(sa, sb)
	if oa {
		f.releaseF(sa)
	}
	if ob {
		f.releaseF(sb)
	}
	result := f.allocReg(0)
	f.a.SetccReg(condNE, result)
	f.pushReg(result, mtI32)
	f.stats.peep("simd-and-anytrue")
	return true
}

// tryV128NotAnd selects `a & ~b` when producers spell it as v128.not followed
// immediately by v128.and. The Wasm v128.andnot opcode and VPANDN use opposite
// source order, so VPANDN(dst,b,a) is the exact one-instruction result.
func (f *fn) tryV128NotAnd(r *wasm.Reader) bool {
	if !simdSuperoptEnabled || !matchNextSIMDOp(r, 78) {
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
	f.a.VPandn(dst, sb, sa)
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
	// Sign-saturate-pack the 8 words to 8 bytes (each byte keeps its word's sign),
	// then VPMOVMSKB gives all 8 lane signs in the low byte.
	v := f.popValue()
	x := f.materializeV128(v)
	packed := f.allocFReg(maskOf(x))
	f.a.VPacksswb(packed, x, x)
	f.releaseF(x)
	r := f.allocReg(0)
	f.a.VPmovmskb(r, packed)
	f.releaseF(packed)
	f.a.AluRI(4, r, 0x00ff, false) // keep the low 8 lane bits
	f.pushReg(r, mtI32)
}

func (f *fn) i32x4Bitmask() {
	v := f.popValue()
	x := f.materializeV128(v)
	r := f.allocReg(0)
	f.a.VMovmskps(r, x) // 4 lane sign bits directly
	f.releaseF(x)
	f.pushReg(r, mtI32)
}

func (f *fn) i64x2Bitmask() {
	v := f.popValue()
	x := f.materializeV128(v)
	r := f.allocReg(0)
	f.a.VMovmskpd(r, x) // 2 lane sign bits directly
	f.releaseF(x)
	f.pushReg(r, mtI32)
}

func (f *fn) v128SplatScalar(r Reg, size int) Reg {
	switch size {
	case 1:
		f.a.AluRI(4, r, 0xff, false) // keep only the low i8 lane, zeroing the high half.
		pat := f.allocReg(maskOf(r))
		f.a.MovImm64(pat, 0x0101010101010101)
		f.a.IMul(r, pat, true)
		f.release(pat)
		x := f.allocFReg(0)
		f.a.MovGprToXmm(x, r, true)
		f.a.Punpcklqdq(x, x)
		return x
	case 2:
		f.a.AluRI(4, r, 0xffff, false)
		pat := f.allocReg(maskOf(r))
		f.a.MovImm64(pat, 0x0001000100010001)
		f.a.IMul(r, pat, true)
		f.release(pat)
		x := f.allocFReg(0)
		f.a.MovGprToXmm(x, r, true)
		f.a.Punpcklqdq(x, x)
		return x
	case 4:
		x := f.allocFReg(0)
		f.a.MovGprToXmm(x, r, false)
		f.a.Pshufd(x, x, 0x00)
		return x
	case 8:
		x := f.allocFReg(0)
		f.a.MovGprToXmm(x, r, true)
		f.a.Punpcklqdq(x, x)
		return x
	}
	panic("amd64: invalid scalar splat width")
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
	f.a.LoadIdx(t, RBX, ea, disp, 8, false, true)
	if eaOwned {
		f.release(ea)
	}
	x := f.allocFReg(0)
	f.a.MovGprToXmm(x, t, true)
	f.release(t)

	switch sub {
	case 1: // v128.load8x8_s
		f.a.VPunpcklbw(x, x, x)
		f.a.VPsrawImm(x, x, 8)
	case 2: // v128.load8x8_u
		z := f.allocFReg(maskOf(x))
		f.a.VPxor(z, z, z)
		f.a.VPunpcklbw(x, x, z)
		f.releaseF(z)
	case 3: // v128.load16x4_s
		f.a.VPunpcklwd(x, x, x)
		f.a.VPsradImm(x, x, 16)
	case 4: // v128.load16x4_u
		z := f.allocFReg(maskOf(x))
		f.a.VPxor(z, z, z)
		f.a.VPunpcklwd(x, x, z)
		f.releaseF(z)
	case 5: // v128.load32x2_s
		z := f.allocFReg(maskOf(x))
		f.a.VPxor(z, z, z)
		sign := f.allocFReg(maskOf(x, z))
		f.a.VPcmpgtd(sign, z, x)
		f.a.VPunpckldq(x, x, sign)
		f.releaseF(sign)
		f.releaseF(z)
	case 6: // v128.load32x2_u
		z := f.allocFReg(maskOf(x))
		f.a.VPxor(z, z, z)
		f.a.VPunpckldq(x, x, z)
		f.releaseF(z)
	default:
		panic("amd64: invalid SIMD load-extend opcode")
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
	panic("amd64: invalid SIMD load-splat opcode")
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
	f.a.LoadIdx(t, RBX, ea, disp, size, false, size == 8)
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
	panic("amd64: invalid SIMD load-zero opcode")
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
	f.a.LoadIdx(t, RBX, ea, disp, size, false, size == 8)
	if eaOwned {
		f.release(ea)
	}
	x := f.allocFReg(0)
	f.a.MovGprToXmm(x, t, size == 8)
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
	panic("amd64: invalid SIMD lane memory opcode")
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
	f.a.LoadIdx(t, RBX, ea, disp, size, false, size == 8)
	if eaOwned {
		f.release(ea)
	}
	f.fpinned = f.fpinned.remove(x)
	switch size {
	case 1:
		f.a.Pinsrb(x, t, lane)
	case 2:
		f.a.Pinsrw(x, t, lane)
	case 4:
		f.a.Pinsrd(x, t, lane)
	case 8:
		f.a.Pinsrq(x, t, lane)
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

	f.materializePendingLoads()
	v := f.popValue()
	x := f.materializeV128(v)
	f.fpinned = f.fpinned.add(x)
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	t := f.allocReg(0)
	switch size {
	case 1:
		f.a.Pextrb(t, x, lane)
	case 2:
		f.a.Pextrw(t, x, lane)
	case 4:
		f.a.Pextrd(t, x, lane)
	case 8:
		f.a.Pextrq(t, x, lane)
	}
	f.a.StoreIdx(RBX, ea, t, disp, size)
	f.release(t)
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
				return fmt.Errorf("amd64: invalid i8x16.shuffle lane %d", lane)
			}
			lanes[i] = lane
		}
		f.i8x16Shuffle(lanes)
	case 14: // i8x16.swizzle
		f.i8x16Swizzle()
	case 256: // i8x16.relaxed_swizzle: deterministic raw PSHUFB semantics.
		f.v128Bin(r, f.a.VPshufb)
	case 257: // i32x4.relaxed_trunc_f32x4_s: conservative saturating choice.
		f.v128I32x4TruncSat(false, true)
	case 258: // i32x4.relaxed_trunc_f32x4_u: conservative saturating choice.
		f.v128I32x4TruncSat(false, false)
	case 259: // i32x4.relaxed_trunc_f64x2_s_zero: conservative saturating choice.
		f.v128I32x4TruncSat(true, true)
	case 260: // i32x4.relaxed_trunc_f64x2_u_zero: conservative saturating choice.
		f.v128I32x4TruncSat(true, false)
	case 261: // f32x4.relaxed_madd: deterministic MULPS + ADDPS choice.
		f.v128RelaxedMadd(false, false)
	case 262: // f32x4.relaxed_nmadd: deterministic MULPS then subtract from addend.
		f.v128RelaxedMadd(false, true)
	case 263: // f64x2.relaxed_madd: deterministic MULPD + ADDPD choice.
		f.v128RelaxedMadd(true, false)
	case 264: // f64x2.relaxed_nmadd: deterministic MULPD then subtract from addend.
		f.v128RelaxedMadd(true, true)
	case 265, 266, 267, 268: // relaxed_laneselect: deterministic bitselect choice.
		f.v128Bitselect()
	case 269: // f32x4.relaxed_min: deterministic native MINPS choice.
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMin(dst, s1, s2, false) })
	case 270: // f32x4.relaxed_max: deterministic native MAXPS choice.
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMax(dst, s1, s2, false) })
	case 271: // f64x2.relaxed_min: deterministic native MINPD choice.
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMin(dst, s1, s2, true) })
	case 272: // f64x2.relaxed_max: deterministic native MAXPD choice.
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMax(dst, s1, s2, true) })
	case 273: // i16x8.relaxed_q15mulr_s: deterministic raw PMULHRSW choice.
		f.v128Bin(r, f.a.VPmulhrsw)
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
		f.v128Bin(r, f.a.VPcmpeqb)
	case 36: // i8x16.ne
		f.v128BinNot(f.a.VPcmpeqb)
	case 37: // i8x16.lt_s
		f.v128SignedCmp(f.a.VPcmpgtb, true, false)
	case 38: // i8x16.lt_u
		f.v128UnsignedCmp(f.a.VPmaxub, f.a.VPcmpeqb, true)
	case 39: // i8x16.gt_s
		f.v128Bin(r, f.a.VPcmpgtb)
	case 40: // i8x16.gt_u
		f.v128UnsignedCmp(f.a.VPminub, f.a.VPcmpeqb, true)
	case 41: // i8x16.le_s
		f.v128SignedCmp(f.a.VPcmpgtb, false, true)
	case 42: // i8x16.le_u
		f.v128UnsignedCmp(f.a.VPminub, f.a.VPcmpeqb, false)
	case 43: // i8x16.ge_s
		f.v128SignedCmp(f.a.VPcmpgtb, true, true)
	case 44: // i8x16.ge_u
		f.v128UnsignedCmp(f.a.VPmaxub, f.a.VPcmpeqb, false)
	case 45: // i16x8.eq
		f.v128Bin(r, f.a.VPcmpeqw)
	case 46: // i16x8.ne
		f.v128BinNot(f.a.VPcmpeqw)
	case 47: // i16x8.lt_s
		f.v128SignedCmp(f.a.VPcmpgtw, true, false)
	case 48: // i16x8.lt_u
		f.v128UnsignedCmp(f.a.VPmaxuw, f.a.VPcmpeqw, true)
	case 49: // i16x8.gt_s
		f.v128Bin(r, f.a.VPcmpgtw)
	case 50: // i16x8.gt_u
		f.v128UnsignedCmp(f.a.VPminuw, f.a.VPcmpeqw, true)
	case 51: // i16x8.le_s
		f.v128SignedCmp(f.a.VPcmpgtw, false, true)
	case 52: // i16x8.le_u
		f.v128UnsignedCmp(f.a.VPminuw, f.a.VPcmpeqw, false)
	case 53: // i16x8.ge_s
		f.v128SignedCmp(f.a.VPcmpgtw, true, true)
	case 54: // i16x8.ge_u
		f.v128UnsignedCmp(f.a.VPmaxuw, f.a.VPcmpeqw, false)
	case 55: // i32x4.eq
		f.v128Bin(r, f.a.VPcmpeqd)
	case 56: // i32x4.ne
		f.v128BinNot(f.a.VPcmpeqd)
	case 57: // i32x4.lt_s
		f.v128SignedCmp(f.a.VPcmpgtd, true, false)
	case 58: // i32x4.lt_u
		f.v128UnsignedCmp(f.a.VPmaxud, f.a.VPcmpeqd, true)
	case 59: // i32x4.gt_s
		f.v128Bin(r, f.a.VPcmpgtd)
	case 60: // i32x4.gt_u
		f.v128UnsignedCmp(f.a.VPminud, f.a.VPcmpeqd, true)
	case 61: // i32x4.le_s
		f.v128SignedCmp(f.a.VPcmpgtd, false, true)
	case 62: // i32x4.le_u
		f.v128UnsignedCmp(f.a.VPminud, f.a.VPcmpeqd, false)
	case 63: // i32x4.ge_s
		f.v128SignedCmp(f.a.VPcmpgtd, true, true)
	case 64: // i32x4.ge_u
		f.v128UnsignedCmp(f.a.VPmaxud, f.a.VPcmpeqd, false)
	case 65: // f32x4.eq
		f.v128FCmp(r, false, vfcmpEqOQ)
	case 66: // f32x4.ne
		f.v128FCmp(r, false, vfcmpNeqUQ)
	case 67: // f32x4.lt
		f.v128FCmp(r, false, vfcmpLtOQ)
	case 68: // f32x4.gt
		f.v128FCmp(r, false, vfcmpGtOQ)
	case 69: // f32x4.le
		f.v128FCmp(r, false, vfcmpLeOQ)
	case 70: // f32x4.ge
		f.v128FCmp(r, false, vfcmpGeOQ)
	case 71: // f64x2.eq
		f.v128FCmp(r, true, vfcmpEqOQ)
	case 72: // f64x2.ne
		f.v128FCmp(r, true, vfcmpNeqUQ)
	case 73: // f64x2.lt
		f.v128FCmp(r, true, vfcmpLtOQ)
	case 74: // f64x2.gt
		f.v128FCmp(r, true, vfcmpGtOQ)
	case 75: // f64x2.le
		f.v128FCmp(r, true, vfcmpLeOQ)
	case 76: // f64x2.ge
		f.v128FCmp(r, true, vfcmpGeOQ)
	case 101: // i8x16.narrow_i16x8_s
		f.v128Bin(r, f.a.VPpacksswb)
	case 102: // i8x16.narrow_i16x8_u
		f.v128Bin(r, f.a.VPpackuswb)
	case 103: // f32x4.ceil
		f.v128FloatRound(false, roundCeil)
	case 104: // f32x4.floor
		f.v128FloatRound(false, roundFloor)
	case 105: // f32x4.trunc
		f.v128FloatRound(false, roundTrunc)
	case 106: // f32x4.nearest
		f.v128FloatRound(false, roundNearest)
	case 107: // i8x16.shl
		f.i8x16Shift(f.a.VPsllw, i8ShiftShl)
	case 108: // i8x16.shr_s
		f.i8x16Shift(f.a.VPsraw, i8ShiftShrS)
	case 109: // i8x16.shr_u
		f.i8x16Shift(f.a.VPsrlw, i8ShiftShrU)
	case 110: // i8x16.add
		f.v128Bin(r, f.a.VPaddb)
	case 111: // i8x16.add_sat_s
		f.v128Bin(r, f.a.VPaddsb)
	case 112: // i8x16.add_sat_u
		f.v128Bin(r, f.a.VPaddusb)
	case 113: // i8x16.sub
		f.v128Bin(r, f.a.VPsubb)
	case 114: // i8x16.sub_sat_s
		f.v128Bin(r, f.a.VPsubsb)
	case 115: // i8x16.sub_sat_u
		f.v128Bin(r, f.a.VPsubusb)
	case 116: // f64x2.ceil
		f.v128FloatRound(true, roundCeil)
	case 117: // f64x2.floor
		f.v128FloatRound(true, roundFloor)
	case 118: // i8x16.min_s
		f.v128Bin(r, f.a.VPminsb)
	case 119: // i8x16.min_u
		f.v128Bin(r, f.a.VPminub)
	case 120: // i8x16.max_s
		f.v128Bin(r, f.a.VPmaxsb)
	case 121: // i8x16.max_u
		f.v128Bin(r, f.a.VPmaxub)
	case 122: // f64x2.trunc
		f.v128FloatRound(true, roundTrunc)
	case 123: // i8x16.avgr_u
		f.v128Bin(r, f.a.VPavgb)
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
		f.v128Bin(r, f.a.VPpackssdw)
	case 134: // i16x8.narrow_i32x4_u
		f.v128Bin(r, f.a.VPpackusdw)
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
		f.v128Bin(r, f.a.VPaddw)
	case 143: // i16x8.add_sat_s
		f.v128Bin(r, f.a.VPaddsw)
	case 144: // i16x8.add_sat_u
		f.v128Bin(r, f.a.VPaddusw)
	case 145: // i16x8.sub
		f.v128Bin(r, f.a.VPsubw)
	case 146: // i16x8.sub_sat_s
		f.v128Bin(r, f.a.VPsubsw)
	case 147: // i16x8.sub_sat_u
		f.v128Bin(r, f.a.VPsubusw)
	case 148: // f64x2.nearest
		f.v128FloatRound(true, roundNearest)
	case 149: // i16x8.mul
		f.v128Bin(r, f.a.VPmullw)
	case 150: // i16x8.min_s
		f.v128Bin(r, f.a.VPminsw)
	case 151: // i16x8.min_u
		f.v128Bin(r, f.a.VPminuw)
	case 152: // i16x8.max_s
		f.v128Bin(r, f.a.VPmaxsw)
	case 153: // i16x8.max_u
		f.v128Bin(r, f.a.VPmaxuw)
	case 155: // i16x8.avgr_u
		f.v128Bin(r, f.a.VPavgw)
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
	case 204: // i64x2.shr_s
		f.i64x2ShrS()
	case 205: // i64x2.shr_u
		f.i64x2Shift(f.a.VPsrlq)
	case 174: // i32x4.add
		f.v128Bin(r, f.a.VPaddd)
	case 177: // i32x4.sub
		f.v128Bin(r, f.a.VPsubd)
	case 181: // i32x4.mul
		f.v128Bin(r, f.a.VPmulld)
	case 182: // i32x4.min_s
		f.v128Bin(r, f.a.VPminsd)
	case 183: // i32x4.min_u
		f.v128Bin(r, f.a.VPminud)
	case 184: // i32x4.max_s
		f.v128Bin(r, f.a.VPmaxsd)
	case 185: // i32x4.max_u
		f.v128Bin(r, f.a.VPmaxud)
	case 186: // i32x4.dot_i16x8_s
		f.v128Bin(r, f.a.VPmaddwd)
	case 188: // i32x4.extmul_low_i16x8_s
		f.i32x4ExtmulI16x8(true, false)
	case 189: // i32x4.extmul_high_i16x8_s
		f.i32x4ExtmulI16x8(true, true)
	case 190: // i32x4.extmul_low_i16x8_u
		f.i32x4ExtmulI16x8(false, false)
	case 191: // i32x4.extmul_high_i16x8_u
		f.i32x4ExtmulI16x8(false, true)
	case 206: // i64x2.add
		f.v128Bin(r, f.a.VPaddq)
	case 209: // i64x2.sub
		f.v128Bin(r, f.a.VPsubq)
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
		f.v128Bin(r, f.a.VPcmpeqq)
	case 215: // i64x2.ne
		f.v128BinNot(f.a.VPcmpeqq)
	case 216: // i64x2.lt_s
		f.i64x2SignedCmp(condL)
	case 217: // i64x2.gt_s
		f.i64x2SignedCmp(condG)
	case 218: // i64x2.le_s
		f.i64x2SignedCmp(condLE)
	case 219: // i64x2.ge_s
		f.i64x2SignedCmp(condGE)
	case 224: // f32x4.abs
		f.v128FloatSignOp(false, true, 0x54)
	case 225: // f32x4.neg
		f.v128FloatSignOp(false, false, 0x57)
	case 227: // f32x4.sqrt
		f.v128IntegerAbs(func(dst, src Reg) { f.a.VFPackedSqrt(dst, src, false) })
	case 228: // f32x4.add
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedAdd(dst, s1, s2, false) })
	case 229: // f32x4.sub
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedSub(dst, s1, s2, false) })
	case 230: // f32x4.mul
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMul(dst, s1, s2, false) })
	case 231: // f32x4.div
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedDiv(dst, s1, s2, false) })
	case 232: // f32x4.min
		f.v128FloatMinMax(false, false)
	case 233: // f32x4.max
		f.v128FloatMinMax(false, true)
	case 234: // f32x4.pmin: deterministic pseudo-min with first operand winning equal/NaN-second lanes.
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMin(dst, s2, s1, false) })
	case 235: // f32x4.pmax: deterministic pseudo-max with first operand winning equal/NaN-second lanes.
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMax(dst, s2, s1, false) })
	case 236: // f64x2.abs
		f.v128FloatSignOp(true, true, 0x54)
	case 237: // f64x2.neg
		f.v128FloatSignOp(true, false, 0x57)
	case 239: // f64x2.sqrt
		f.v128IntegerAbs(func(dst, src Reg) { f.a.VFPackedSqrt(dst, src, true) })
	case 240: // f64x2.add
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedAdd(dst, s1, s2, true) })
	case 241: // f64x2.sub
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedSub(dst, s1, s2, true) })
	case 242: // f64x2.mul
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMul(dst, s1, s2, true) })
	case 243: // f64x2.div
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedDiv(dst, s1, s2, true) })
	case 244: // f64x2.min
		f.v128FloatMinMax(true, false)
	case 245: // f64x2.max
		f.v128FloatMinMax(true, true)
	case 246: // f64x2.pmin: deterministic pseudo-min with first operand winning equal/NaN-second lanes.
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMin(dst, s2, s1, true) })
	case 247: // f64x2.pmax: deterministic pseudo-max with first operand winning equal/NaN-second lanes.
		f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.VFPackedMax(dst, s2, s1, true) })
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
	case 192: // i64x2.abs
		f.i64x2Abs()
	case 193: // i64x2.neg
		f.v128IntegerNeg(f.a.VPsubq)
	case 77: // v128.not
		if f.tryV128NotAnd(r) {
			break
		}
		f.v128UnaryNot()
	case 78: // v128.and
		if f.tryV128AndAnyTrue(r) {
			break
		}
		f.v128Bin(r, f.a.VPand)
	case 79: // v128.andnot (a & ~b). VPANDN(dst, s1, s2) = ~s1 & s2, so
		// VPANDN(dst, b, a) = ~b & a = the Wasm result in one instruction.
		b := f.popValue()
		a := f.popValue()
		sa, oa := f.operandRegV128(a)
		f.fpinned = f.fpinned.add(sa)
		sb, ob := f.operandRegV128(b)
		f.fpinned = f.fpinned.remove(sa)
		dst := f.allocFReg(maskOf(sa, sb))
		f.a.VPandn(dst, sb, sa)
		if oa {
			f.releaseF(sa)
		}
		if ob {
			f.releaseF(sb)
		}
		f.pushVReg(dst)
	case 80: // v128.or
		f.v128Bin(r, f.a.VPor)
	case 81: // v128.xor
		f.v128Bin(r, f.a.VPxor)
	case 82: // v128.bitselect: (a & mask) | (b & ~mask)
		f.v128Bitselect()
	default:
		return fmt.Errorf("amd64: unsupported 0xFD opcode %d", sub)
	}
	return nil
}
