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

// v128ConstReg returns a fresh OWNED V register holding the 128-bit constant
// (lo,hi). When the value was cached in a reserved register at entry (a repeated
// const, see preloadV128Consts), the fresh register is filled with a single
// MOV.16b copy instead of rebuilding the immediate word by word.
func (f *fn) v128ConstReg(lo, hi uint64) Reg {
	x := f.allocFReg(0)
	if lo == 0 && hi == 0 {
		f.a.NeonEor16b(x, x, x)
		return x
	}
	if c, ok := f.v128ConstCached(lo, hi); ok {
		f.a.NeonMov16b(x, c)
		return x
	}
	f.buildV128Const(x, lo, hi)
	return x
}

// buildV128Const materializes the 128-bit constant (lo,hi) into V register x via a
// GP scratch: FMOV Dn,Xt sets the low 64 (and zeroes the high half), then an INS
// writes the high 64 when non-zero.
func (f *fn) buildV128Const(x Reg, lo, hi uint64) {
	t := f.allocReg(0)
	f.a.MovImm64(t, lo)
	f.a.FmovFromGpr(x, t, true) // FMOV Dn,Xt zeroes the high 64 bits.
	if hi != 0 {
		f.a.MovImm64(t, hi)
		f.a.NeonInsD(x, t, 1)
	}
	f.release(t)
}

// v128ConstReg holds a v128.const value cached in a reserved V register.
type v128ConstReg struct {
	lo, hi uint64
	reg    Reg
}

// maxV128Consts bounds how many distinct repeated v128 constants are pinned in
// reserved V registers per function. The isa_simd_reduce corpus needs one; two
// covers dual-const loops without meaningfully shrinking the 32-entry V pool.
const maxV128Consts = 2

// v128ConstMask blocks the reserved const registers from the V allocator, exactly
// like fconstMask for scalar-float constants.
func (f *fn) v128ConstMask() regMask {
	var m regMask
	for _, c := range f.vconsts {
		m = m.add(c.reg)
	}
	return m
}

// pinnedV128LocalCount counts the v128 locals held in a dedicated V register for
// the whole function. It gauges the loop's baseline NEON register pressure, used to
// decide whether reserving another V register for a cached const is worthwhile.
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

// v128ConstCached returns the reserved register holding (lo,hi), if any.
func (f *fn) v128ConstCached(lo, hi uint64) (Reg, bool) {
	for _, c := range f.vconsts {
		if c.lo == lo && c.hi == hi {
			return c.reg, true
		}
	}
	return regNone, false
}

// preloadV128Consts scans the function body for v128.const immediates that appear
// more than once and reserves a V register for each (up to maxV128Consts),
// materializing the value once at entry. It mirrors preloadFloatConsts. Disabled
// for call-making functions: a wasm→wasm call preserves only the low 64 bits of
// the callee-saved V range, so a 128-bit reserved const could not survive a call.
func (f *fn) preloadV128Consts(code []byte) {
	if f.usesCalls || !v128ConstCacheEnabled {
		return
	}
	// Register-pressure gate. Reserving a V register for a cached const is only a win
	// when the vector loop has spare NEON registers. Functions that pin two or more
	// v128 locals (the coupled `local.set $a/$b` accumulator loops) already hold high
	// v128 liveness, and funneling every use of a repeated const through one reserved
	// register serializes the loop instead of helping (measured ~2.3x on M4 for
	// v128.bitselect). The scalar-producing reductions this cache targets pin no v128
	// locals, so this leaves their win intact.
	if f.pinnedV128LocalCount() >= 2 {
		return
	}
	// Tally distinct v128 consts in a small fixed buffer (no allocation on the hot
	// compile path). Extra distinct consts beyond the buffer are ignored — only the
	// first few candidates can win a reserved register anyway.
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
		// Any other SIMD op: rewind past the prefix and let the shared skipper consume
		// the sub-opcode and its immediates.
		if err := r.JumpTo(afterPrefix); err != nil {
			return
		}
		if err := wasm.SkipInstructionImmediate(r, op); err != nil {
			return
		}
	}
	for i := 0; i < nCand && len(f.vconsts) < maxV128Consts; i++ {
		if cand[i].n < 2 || (cand[i].lo == 0 && cand[i].hi == 0) {
			continue // single-use, or the zero const (already a single EOR)
		}
		x := f.allocFReg(0)
		f.buildV128Const(x, cand[i].lo, cand[i].hi)
		f.vconsts = append(f.vconsts, v128ConstReg{lo: cand[i].lo, hi: cand[i].hi, reg: x})
	}
}

func (f *fn) v128Const(lo, hi uint64) {
	f.pushVReg(f.v128ConstReg(lo, hi))
}

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

func v128MaskBits(b [16]byte) (uint64, uint64) {
	return binary.LittleEndian.Uint64(b[0:8]), binary.LittleEndian.Uint64(b[8:16])
}

func (f *fn) i8x16Swizzle() {
	// result[i] = (idx[i] < 16) ? src[idx[i]] : 0 is exactly TBL Vd.16b,{Vn.16b},Vm.16b
	// (a single-register table; out-of-range indices produce 0). Both operands are
	// read-only, so use them in place — a pinned v128 local needs no owned copy — and
	// write into a fresh destination.
	idxElem := f.popValue()
	srcElem := f.popValue()
	src, srcOwned := f.operandRegV128(srcElem)
	f.fpinned = f.fpinned.add(src)
	idx, idxOwned := f.operandRegV128(idxElem)
	f.fpinned = f.fpinned.add(idx)
	dst := f.allocFReg(maskOf(src, idx))
	f.a.NeonTbl(dst, src, idx)
	f.fpinned = f.fpinned.remove(src).remove(idx)
	if idxOwned {
		f.releaseF(idx)
	}
	if srcOwned {
		f.releaseF(src)
	}
	f.pushVReg(dst)
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

// v128Bin lowers a two-operand v128 op. When the op is immediately consumed by
// `local.set/tee $x` into a register-pinned v128 local, tryV128BinLocalSet emits
// it in place into $x's V register (one instruction, no accumulator copy and no
// result-to-pin copy). Otherwise it falls back to the eager form: an owned
// writable copy of the left operand that the op accumulates into.
func (f *fn) v128Bin(r *wasm.Reader, op func(dst, s1, s2 Reg)) error {
	if done, err := f.tryV128BinLocalSet(r, op); done || err != nil {
		return err
	}
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
	return nil
}

// v128BinInto emits op(dst, s1, s2) reading BOTH operands in place — dst is a
// pinned v128 local's V register the result sinks into. No owned copy of the left
// operand (the eager path's accumulator copy) and no trailing result-to-pin move.
// A NEON 3-operand op reads both source registers before writing dst, so any
// aliasing among dst/s1/s2 (e.g. the accumulator `x = x op y`, or `x = x op x`) is
// correct. Mirrors fbinInto.
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
// register-pinned v128 local and sinks the NEON op straight into $x's V register.
// It is the SIMD twin of tryFbinLocalSet: A and B are realized into registers
// (read in place when they are pinned locals) before the op overwrites $x, and any
// operand-stack reference to $x BELOW this expression is realized first
// (local.get-at-read-time). Returns done=true when it fired (and consumed the
// local.set/tee), restoring the reader on any mismatch.
func (f *fn) tryV128BinLocalSet(r *wasm.Reader, op func(dst, s1, s2 Reg)) (bool, error) {
	if !v128LocalSinkEnabled {
		return false, nil
	}
	save := r.Offset()
	nb, ok := r.Peek()
	if !ok || (nb != 0x21 && nb != 0x22) { // local.set / local.tee
		return false, nil
	}
	if _, err := r.Byte(); err != nil {
		return false, err
	}
	x32, err := r.U32()
	if err != nil {
		return false, err
	}
	x := int(x32) + f.localBase
	pr, _, pinned := f.pinReg(x)
	if !pinned || x < 0 || x >= len(f.localType) || f.localType[x] != mtV128 {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	if f.bcKind == 1 && f.bcIdx == uint32(x) {
		f.invalidateBoundsCert()
	}
	right := f.s.back()
	if right == nil {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
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
	return true, nil
}

// v128Unary lowers a single-instruction unary v128 op (op(dst, src)). When
// consumed by `local.set/tee $x` into a pinned v128 local, it sinks in place;
// otherwise it materializes the operand (an owned copy for a pinned local) and
// rewrites it.
func (f *fn) v128Unary(r *wasm.Reader, op func(dst, src Reg)) error {
	if done, err := f.tryV128UnaryLocalSet(r, op); done || err != nil {
		return err
	}
	a := f.popValue()
	x := f.materializeV128(a)
	op(x, x)
	f.pushVReg(x)
	return nil
}

// v128UnaryInto emits op(dst, src) reading src in place — dst is a pinned v128
// local's V register. The op reads src before writing dst, so src==dst (the
// `x = unop(x)` accumulator) is correct.
func (f *fn) v128UnaryInto(dst Reg, op func(dst, src Reg)) {
	a := f.popValue()
	src, owned := f.operandRegV128(a)
	op(dst, src)
	if owned && dst != src {
		f.releaseF(src)
	}
}

// tryV128UnaryLocalSet is the unary companion to tryV128BinLocalSet.
func (f *fn) tryV128UnaryLocalSet(r *wasm.Reader, op func(dst, src Reg)) (bool, error) {
	if !v128LocalSinkEnabled {
		return false, nil
	}
	save := r.Offset()
	nb, ok := r.Peek()
	if !ok || (nb != 0x21 && nb != 0x22) {
		return false, nil
	}
	if _, err := r.Byte(); err != nil {
		return false, err
	}
	x32, err := r.U32()
	if err != nil {
		return false, err
	}
	x := int(x32) + f.localBase
	pr, _, pinned := f.pinReg(x)
	if !pinned || x < 0 || x >= len(f.localType) || f.localType[x] != mtV128 {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	if f.bcKind == 1 && f.bcIdx == uint32(x) {
		f.invalidateBoundsCert()
	}
	right := f.s.back()
	if right == nil {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	// Realize refs to $x below the operand block; the operand is consumed in place.
	f.realizeLocalRefs(x, baseOfValentBlock(right))
	f.v128UnaryInto(pr, op)
	f.markLocalDirty(x)
	f.stats.peep("v128-local-sink")
	if nb == 0x22 {
		f.pushValue(storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: x})
	}
	return true, nil
}

// v128NarrowInto sinks a two-source saturating narrow into the pinned dst:
// low(dst)=sqxtn(a), high(dst)=sqxtn2(b). SQXTN writes dst's low half (clearing
// the high half); SQXTN2 then writes the high half while READING dst's low half,
// so a's narrow must land in dst first. Because that first write overwrites dst,
// if the high source b aliases dst its value would be destroyed, so it is
// snapshotted into a scratch register beforehand. a==dst is safe (SQXTN reads dst
// before writing it).
func (f *fn) v128NarrowInto(dst Reg, sqxtn, sqxtn2 func(dst, src Reg)) {
	b := f.popValue()
	a := f.popValue()
	s1, o1 := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(s1)
	s2, o2 := f.operandRegV128(b)
	if s2 == dst {
		// SQXTN(dst, s1) will clobber dst, which is also b's source; snapshot b.
		t := f.allocFReg(maskOf(s1, dst))
		f.a.NeonOrr16b(t, s2, s2)
		if o2 {
			f.releaseF(s2)
		}
		s2, o2 = t, true
	}
	f.fpinned = f.fpinned.remove(s1)
	sqxtn(dst, s1)
	sqxtn2(dst, s2)
	if o1 && dst != s1 {
		f.releaseF(s1)
	}
	if o2 && dst != s2 {
		f.releaseF(s2)
	}
}

func (f *fn) v128NarrowI16x8ToI8x16(r *wasm.Reader, signed bool) error {
	sqxtn, sqxtn2 := f.a.NeonSqxtnBfromH, f.a.NeonSqxtn2BfromH
	if !signed {
		sqxtn, sqxtn2 = f.a.NeonSqxtunBfromH, f.a.NeonSqxtun2BfromH
	}
	if done, err := f.tryV128Sink2LocalSet(r, func(dst Reg) {
		f.v128NarrowInto(dst, sqxtn, sqxtn2)
	}); done || err != nil {
		return err
	}
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	sqxtn(xa, xa)
	sqxtn2(xa, xb)
	f.fpinned = f.fpinned.remove(xa)
	f.releaseF(xb)
	f.pushVReg(xa)
	return nil
}

func (f *fn) v128NarrowI32x4ToI16x8(r *wasm.Reader, signed bool) error {
	sqxtn, sqxtn2 := f.a.NeonSqxtnHfromS, f.a.NeonSqxtn2HfromS
	if !signed {
		sqxtn, sqxtn2 = f.a.NeonSqxtunHfromS, f.a.NeonSqxtun2HfromS
	}
	if done, err := f.tryV128Sink2LocalSet(r, func(dst Reg) {
		f.v128NarrowInto(dst, sqxtn, sqxtn2)
	}); done || err != nil {
		return err
	}
	b := f.popValue()
	a := f.popValue()
	xa := f.materializeV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb := f.materializeV128(b)
	sqxtn(xa, xa)
	sqxtn2(xa, xb)
	f.fpinned = f.fpinned.remove(xa)
	f.releaseF(xb)
	f.pushVReg(xa)
	return nil
}

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
	if done, err := f.tryV128Sink2LocalSet(r, func(dst Reg) {
		f.v128PMinMaxInto(dst, f64, isMax)
	}); done || err != nil {
		return err
	}
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

// v128PMinMaxInto sinks pmin/pmax (FCMP selector + BSL) into the pinned dst. The
// selector mask lives in dst itself: FCMP(dst, xb, xa) then BSL(dst, xb, xa)
// yields dst = (xb<a?xb:xa) with no scratch register and no result copy. BSL uses
// dst as BOTH the selector and the output, so the operands xa/xb must survive the
// FCMP write into dst; that holds only when neither aliases dst. When an operand
// IS the pinned local, fall back to a scratch mask and copy the result in. The
// exact FCMP+BSL order/predicate is preserved, so NaN and signed-zero semantics
// (a wins equal/unordered lanes) are unchanged.
func (f *fn) v128PMinMaxInto(dst Reg, f64, isMax bool) {
	bElem := f.popValue()
	aElem := f.popValue()
	xa, oa := f.operandRegV128(aElem)
	f.fpinned = f.fpinned.add(xa)
	xb, ob := f.operandRegV128(bElem)
	f.fpinned = f.fpinned.remove(xa)

	pred := byte(vfcmpLtOQ)
	if isMax {
		pred = vfcmpGtOQ
	}
	if xa != dst && xb != dst {
		f.a.NeonFcmp(dst, xb, xa, f64, pred)
		f.a.NeonBsl16b(dst, xb, xa)
	} else {
		f.fpinned = f.fpinned.add(xa).add(xb)
		mask := f.allocFReg(maskOf(xa, xb, dst))
		f.fpinned = f.fpinned.remove(xa).remove(xb)
		f.a.NeonFcmp(mask, xb, xa, f64, pred)
		f.a.NeonBsl16b(mask, xb, xa)
		f.a.NeonOrr16b(dst, mask, mask)
		f.releaseF(mask)
	}
	if oa && dst != xa {
		f.releaseF(xa)
	}
	if ob && dst != xb {
		f.releaseF(xb)
	}
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
// high half). Both forms read src before the first write, so it sinks through the
// unary path (v128UnaryInto) with src==dst handled correctly. FCVTZ supplies
// Wasm's required NaN->0 and int-min/max overflow saturation.
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

func (f *fn) v128Shift(r *wasm.Reader, op func(dst, s1, s2 Reg), countMask int32, laneSize int, right bool) error {
	if done, err := f.tryV128Sink2LocalSet(r, func(dst Reg) {
		f.v128ShiftInto(dst, op, countMask, laneSize, right)
	}); done || err != nil {
		return err
	}
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
	return nil
}

// v128ShiftInto sinks a vector shift into the pinned dst: the vector operand is
// read in place, the scalar count is masked/negated and splatted, and op(dst,
// value, countX) writes dst. The splat register is allocated while both the
// pinned dst (via fpinnedLocalMask) and the in-place source are protected, so it
// can never alias either; the NEON shift reads both sources before writing dst,
// so value==dst (the `x = x shl c` accumulator) is also correct.
func (f *fn) v128ShiftInto(dst Reg, op func(dst, s1, s2 Reg), countMask int32, laneSize int, right bool) {
	countElem := f.popValue()
	count := f.materialize(countElem)
	f.andImm(count, int64(countMask), false)
	if right {
		f.a.Sub64(count, ZR, count)
	}

	value := f.popValue()
	src, owned := f.operandRegV128(value)
	f.fpinned = f.fpinned.add(src)
	countX := f.v128SplatScalar(count, laneSize)
	f.release(count)
	f.fpinned = f.fpinned.remove(src)

	op(dst, src, countX)
	f.releaseF(countX)
	if owned && dst != src {
		f.releaseF(src)
	}
}

func (f *fn) i8x16Shift(r *wasm.Reader, op func(dst, s1, s2 Reg), right bool) error {
	return f.v128Shift(r, op, 7, 1, right)
}

func (f *fn) i16x8Shift(r *wasm.Reader, op func(dst, s1, s2 Reg), right bool) error {
	return f.v128Shift(r, op, 15, 2, right)
}

func (f *fn) i32x4Shift(r *wasm.Reader, op func(dst, s1, s2 Reg), right bool) error {
	return f.v128Shift(r, op, 31, 4, right)
}

func (f *fn) i64x2Shift(r *wasm.Reader, op func(dst, s1, s2 Reg), right bool) error {
	return f.v128Shift(r, op, 63, 8, right)
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
	if done, err := f.tryV128Sink2LocalSet(r, f.i64x2MulInto); done || err != nil {
		return err
	}
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
	lo := f.allocFReg(maskOf(xa, xb, t, aLo, bLo))
	f.a.NeonUmullDfromS(lo, aLo, bLo) // lo = aLo·bLo widened per 64-bit lane
	f.releaseF(bLo)
	f.releaseF(aLo)
	f.fpinned = f.fpinned.remove(t)
	f.a.NeonAddD(dst, t, lo) // dst written last; reads only t and lo
	f.releaseF(lo)
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

// i16x8ExtmulI8x16 is a single widening-multiply op (SMULL/UMULL for the low
// lanes, SMULL2/UMULL2 for the high lanes): one NEON instruction reading both
// sources' relevant halves before writing dst. It sinks through the ordinary
// binary path (v128BinInto), so aliasing dst==source is safe.
func (f *fn) i16x8ExtmulI8x16(r *wasm.Reader, signed, high bool) error {
	var op func(dst, s1, s2 Reg)
	switch {
	case signed && high:
		op = f.a.NeonSmull2HfromB
	case signed:
		op = f.a.NeonSmullHfromB
	case high:
		op = f.a.NeonUmull2HfromB
	default:
		op = f.a.NeonUmullHfromB
	}
	return f.v128Bin(r, op)
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

func (f *fn) i32x4ExtaddPairwiseI16x8(r *wasm.Reader, signed bool) error {
	if signed {
		return f.v128Unary(r, f.a.NeonSaddlpSfromH)
	}
	return f.v128Unary(r, f.a.NeonUaddlpSfromH)
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

func (f *fn) i32x4DotI16x8S(r *wasm.Reader) error {
	if done, err := f.tryV128Sink2LocalSet(r, f.i32x4DotI16x8SInto); done || err != nil {
		return err
	}
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
	return nil
}

// i32x4DotI16x8SInto sinks the dot product straight into the pinned local dst.
// The two partial products land in fresh temporaries (t0, t1) so neither source is
// clobbered; the final ADDP reads only those temporaries, so writing dst last is
// correct even when dst aliases a source register (the `local.set $a (dot $a $b)`
// accumulator). Both sources are fully consumed before dst is written.
func (f *fn) i32x4DotI16x8SInto(dst Reg) {
	b := f.popValue()
	a := f.popValue()
	xa, oa := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(xa)
	xb, ob := f.operandRegV128(b)
	f.fpinned = f.fpinned.add(xb)
	t0 := f.allocFReg(maskOf(xa, xb))
	t1 := f.allocFReg(maskOf(xa, xb, t0))
	f.a.NeonSmullSfromH(t0, xa, xb)
	f.a.NeonSmull2SfromH(t1, xa, xb)
	f.a.NeonAddpS(dst, t0, t1)
	f.releaseF(t1)
	f.releaseF(t0)
	f.fpinned = f.fpinned.remove(xa).remove(xb)
	if ob {
		f.releaseF(xb)
	}
	if oa {
		f.releaseF(xa)
	}
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

// tryV128Sink2LocalSet is the peep for two-operand v128 ops whose lowering is not
// a single op(dst,s1,s2) closure — compares that need an operand swap, and
// ne (eq followed by NOT). It mirrors tryV128BinLocalSet: when the op is
// immediately consumed by `local.set/tee $x` into a register-pinned v128 local,
// it realizes refs to $x below the two operand blocks and calls emit(pr) to sink
// the op straight into $x's V register. Returns done=true when it fired.
func (f *fn) tryV128Sink2LocalSet(r *wasm.Reader, emit func(dst Reg)) (bool, error) {
	if !v128LocalSinkEnabled {
		return false, nil
	}
	save := r.Offset()
	nb, ok := r.Peek()
	if !ok || (nb != 0x21 && nb != 0x22) { // local.set / local.tee
		return false, nil
	}
	if _, err := r.Byte(); err != nil {
		return false, err
	}
	x32, err := r.U32()
	if err != nil {
		return false, err
	}
	x := int(x32) + f.localBase
	pr, _, pinned := f.pinReg(x)
	if !pinned || x < 0 || x >= len(f.localType) || f.localType[x] != mtV128 {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	if f.bcKind == 1 && f.bcIdx == uint32(x) {
		f.invalidateBoundsCert()
	}
	right := f.s.back()
	if right == nil {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	// Realize refs to $x below the two operand blocks; the operands themselves are
	// consumed in place by emit.
	left := baseOfValentBlock(right).prev
	f.realizeLocalRefs(x, left)
	emit(pr)
	f.markLocalDirty(x)
	f.stats.peep("v128-local-sink")
	if nb == 0x22 { // local.tee keeps the value on the stack
		f.pushValue(storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: x})
	}
	return true, nil
}

// v128CmpInto emits a swap-aware compare (optionally post-inverted for ne) into
// the pinned dst, reading BOTH operands in place. A NEON compare reads both
// source registers before writing dst, so accumulator aliasing (x = x cmp y, or
// x cmp x) is correct. swap picks the operand order (lt/le lower to swapped
// gt/ge); invert appends a full-width NOT (ne = not(eq)).
func (f *fn) v128CmpInto(dst Reg, op func(dst, s1, s2 Reg), swap, invert bool) {
	b := f.popValue()
	a := f.popValue()
	s1, o1 := f.operandRegV128(a)
	f.fpinned = f.fpinned.add(s1)
	s2, o2 := f.operandRegV128(b)
	f.fpinned = f.fpinned.remove(s1)
	if swap {
		op(dst, s2, s1)
	} else {
		op(dst, s1, s2)
	}
	if o1 && dst != s1 {
		f.releaseF(s1)
	}
	if o2 && dst != s2 {
		f.releaseF(s2)
	}
	if invert {
		f.a.NeonNot16b(dst, dst)
	}
}

// v128BinNot lowers `ne` = not(eq): the compare op, then a full-width NOT. When
// consumed by `local.set/tee $x` into a pinned v128 local it sinks both into $x's
// register with no accumulator/result copies. The NOT is a single MVN (the
// compare result is all-ones/all-zeros per lane, so NOT flips it to ne exactly).
func (f *fn) v128BinNot(r *wasm.Reader, op func(dst, s1, s2 Reg)) error {
	if done, err := f.tryV128Sink2LocalSet(r, func(dst Reg) { f.v128CmpInto(dst, op, false, true) }); done || err != nil {
		return err
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

func (f *fn) v128SignedCmp(r *wasm.Reader, op func(dst, s1, s2 Reg), swap, invert bool) error {
	if done, err := f.tryV128Sink2LocalSet(r, func(dst Reg) { f.v128CmpInto(dst, op, swap, invert) }); done || err != nil {
		return err
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
	f.releaseF(xb)
	if invert {
		f.a.NeonNot16b(xa, xa)
	}
	f.pushVReg(xa)
	return nil
}

func (f *fn) v128UnsignedCmp(r *wasm.Reader, op func(dst, s1, s2 Reg), swap bool) error {
	if done, err := f.tryV128Sink2LocalSet(r, func(dst Reg) { f.v128CmpInto(dst, op, swap, false) }); done || err != nil {
		return err
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
	f.releaseF(xb)
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
	if done, err := f.tryV128Sink2LocalSet(r, func(dst Reg) { f.v128CmpInto(dst, op, swap, false) }); done || err != nil {
		return err
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
	f.a.FmovToGpr(r, x, true) // lane 0 -> bit 0
	f.a.NeonUmovD(hi, x, 1)   // lane 1 -> 0/1
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
	addrLocal, addrOK := localAddressKey(f.s.back())
	ea, eaOwned, _, disp := f.memAddr(off, 16, true)
	f.pinned = f.pinned.add(ea)
	f.materializePendingLoadsBeforeStore(ea, addrLocal, addrOK, disp, 16)
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
	addrLocal, addrOK := localAddressKey(f.s.back())
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	f.pinned = f.pinned.add(ea)
	f.materializePendingLoadsBeforeStore(ea, addrLocal, addrOK, disp, size)
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
		return f.v128Bin(r, f.a.NeonTbl)
	case 257: // i32x4.relaxed_trunc_f32x4_s: conservative saturating choice.
		return f.v128I32x4TruncSat(r, false, true)
	case 258: // i32x4.relaxed_trunc_f32x4_u: conservative saturating choice.
		return f.v128I32x4TruncSat(r, false, false)
	case 259: // i32x4.relaxed_trunc_f64x2_s_zero: conservative saturating choice.
		return f.v128I32x4TruncSat(r, true, true)
	case 260: // i32x4.relaxed_trunc_f64x2_u_zero: conservative saturating choice.
		return f.v128I32x4TruncSat(r, true, false)
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
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFmin(dst, s1, s2, false) })
	case 270: // f32x4.relaxed_max: deterministic native FMAX choice.
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFmax(dst, s1, s2, false) })
	case 271: // f64x2.relaxed_min: deterministic native FMIN choice.
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFmin(dst, s1, s2, true) })
	case 272: // f64x2.relaxed_max: deterministic native FMAX choice.
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFmax(dst, s1, s2, true) })
	case 273: // i16x8.relaxed_q15mulr_s: deterministic raw SQRDMULH choice.
		return f.v128Bin(r, f.a.NeonSqrdmulhH)
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
		return f.v128Bin(r, f.a.NeonCmeqB)
	case 36: // i8x16.ne
		return f.v128BinNot(r, f.a.NeonCmeqB)
	case 37: // i8x16.lt_s
		return f.v128SignedCmp(r, f.a.NeonCmgtB, true, false)
	case 38: // i8x16.lt_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhiB, true)
	case 39: // i8x16.gt_s
		return f.v128Bin(r, f.a.NeonCmgtB)
	case 40: // i8x16.gt_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhiB, false)
	case 41: // i8x16.le_s
		return f.v128SignedCmp(r, f.a.NeonCmgeB, true, false)
	case 42: // i8x16.le_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhsB, true)
	case 43: // i8x16.ge_s
		return f.v128SignedCmp(r, f.a.NeonCmgeB, false, false)
	case 44: // i8x16.ge_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhsB, false)
	case 45: // i16x8.eq
		return f.v128Bin(r, f.a.NeonCmeqH)
	case 46: // i16x8.ne
		return f.v128BinNot(r, f.a.NeonCmeqH)
	case 47: // i16x8.lt_s
		return f.v128SignedCmp(r, f.a.NeonCmgtH, true, false)
	case 48: // i16x8.lt_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhiH, true)
	case 49: // i16x8.gt_s
		return f.v128Bin(r, f.a.NeonCmgtH)
	case 50: // i16x8.gt_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhiH, false)
	case 51: // i16x8.le_s
		return f.v128SignedCmp(r, f.a.NeonCmgeH, true, false)
	case 52: // i16x8.le_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhsH, true)
	case 53: // i16x8.ge_s
		return f.v128SignedCmp(r, f.a.NeonCmgeH, false, false)
	case 54: // i16x8.ge_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhsH, false)
	case 55: // i32x4.eq
		return f.v128Bin(r, f.a.NeonCmeqS)
	case 56: // i32x4.ne
		return f.v128BinNot(r, f.a.NeonCmeqS)
	case 57: // i32x4.lt_s
		return f.v128SignedCmp(r, f.a.NeonCmgtS, true, false)
	case 58: // i32x4.lt_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhiS, true)
	case 59: // i32x4.gt_s
		return f.v128Bin(r, f.a.NeonCmgtS)
	case 60: // i32x4.gt_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhiS, false)
	case 61: // i32x4.le_s
		return f.v128SignedCmp(r, f.a.NeonCmgeS, true, false)
	case 62: // i32x4.le_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhsS, true)
	case 63: // i32x4.ge_s
		return f.v128SignedCmp(r, f.a.NeonCmgeS, false, false)
	case 64: // i32x4.ge_u
		return f.v128UnsignedCmp(r, f.a.NeonCmhsS, false)
	case 65: // f32x4.eq
		return f.v128FCmp(r, false, vfcmpEqOQ)
	case 66: // f32x4.ne
		return f.v128FCmp(r, false, vfcmpNeqUQ)
	case 67: // f32x4.lt
		return f.v128FCmp(r, false, vfcmpLtOQ)
	case 68: // f32x4.gt
		return f.v128FCmp(r, false, vfcmpGtOQ)
	case 69: // f32x4.le
		return f.v128FCmp(r, false, vfcmpLeOQ)
	case 70: // f32x4.ge
		return f.v128FCmp(r, false, vfcmpGeOQ)
	case 71: // f64x2.eq
		return f.v128FCmp(r, true, vfcmpEqOQ)
	case 72: // f64x2.ne
		return f.v128FCmp(r, true, vfcmpNeqUQ)
	case 73: // f64x2.lt
		return f.v128FCmp(r, true, vfcmpLtOQ)
	case 74: // f64x2.gt
		return f.v128FCmp(r, true, vfcmpGtOQ)
	case 75: // f64x2.le
		return f.v128FCmp(r, true, vfcmpLeOQ)
	case 76: // f64x2.ge
		return f.v128FCmp(r, true, vfcmpGeOQ)
	case 101: // i8x16.narrow_i16x8_s
		return f.v128NarrowI16x8ToI8x16(r, true)
	case 102: // i8x16.narrow_i16x8_u
		return f.v128NarrowI16x8ToI8x16(r, false)
	case 103: // f32x4.ceil
		return f.v128FloatRound(r, false, roundCeil)
	case 104: // f32x4.floor
		return f.v128FloatRound(r, false, roundFloor)
	case 105: // f32x4.trunc
		return f.v128FloatRound(r, false, roundTrunc)
	case 106: // f32x4.nearest
		return f.v128FloatRound(r, false, roundNearest)
	case 107: // i8x16.shl
		return f.i8x16Shift(r, f.a.NeonUshlB, false)
	case 108: // i8x16.shr_s
		return f.i8x16Shift(r, f.a.NeonSshrvB, true)
	case 109: // i8x16.shr_u
		return f.i8x16Shift(r, f.a.NeonUshrvB, true)
	case 110: // i8x16.add
		return f.v128Bin(r, f.a.NeonAddB)
	case 111: // i8x16.add_sat_s
		return f.v128Bin(r, f.a.NeonSqaddB)
	case 112: // i8x16.add_sat_u
		return f.v128Bin(r, f.a.NeonUqaddB)
	case 113: // i8x16.sub
		return f.v128Bin(r, f.a.NeonSubB)
	case 114: // i8x16.sub_sat_s
		return f.v128Bin(r, f.a.NeonSqsubB)
	case 115: // i8x16.sub_sat_u
		return f.v128Bin(r, f.a.NeonUqsubB)
	case 116: // f64x2.ceil
		return f.v128FloatRound(r, true, roundCeil)
	case 117: // f64x2.floor
		return f.v128FloatRound(r, true, roundFloor)
	case 118: // i8x16.min_s
		return f.v128Bin(r, f.a.NeonSminB)
	case 119: // i8x16.min_u
		return f.v128Bin(r, f.a.NeonUminB)
	case 120: // i8x16.max_s
		return f.v128Bin(r, f.a.NeonSmaxB)
	case 121: // i8x16.max_u
		return f.v128Bin(r, f.a.NeonUmaxB)
	case 122: // f64x2.trunc
		return f.v128FloatRound(r, true, roundTrunc)
	case 123: // i8x16.avgr_u
		return f.v128Bin(r, f.a.NeonUrhaddB)
	case 124: // i16x8.extadd_pairwise_i8x16_s
		return f.i16x8ExtaddPairwiseI8x16(r, true)
	case 125: // i16x8.extadd_pairwise_i8x16_u
		return f.i16x8ExtaddPairwiseI8x16(r, false)
	case 126: // i32x4.extadd_pairwise_i16x8_s
		return f.i32x4ExtaddPairwiseI16x8(r, true)
	case 127: // i32x4.extadd_pairwise_i16x8_u
		return f.i32x4ExtaddPairwiseI16x8(r, false)
	case 130: // i16x8.q15mulr_sat_s
		return f.i16x8Q15mulrSatS(r)
	case 133: // i16x8.narrow_i32x4_s
		return f.v128NarrowI32x4ToI16x8(r, true)
	case 134: // i16x8.narrow_i32x4_u
		return f.v128NarrowI32x4ToI16x8(r, false)
	case 135: // i16x8.extend_low_i8x16_s
		return f.i16x8ExtendI8x16(r, true, false)
	case 136: // i16x8.extend_high_i8x16_s
		return f.i16x8ExtendI8x16(r, true, true)
	case 137: // i16x8.extend_low_i8x16_u
		return f.i16x8ExtendI8x16(r, false, false)
	case 138: // i16x8.extend_high_i8x16_u
		return f.i16x8ExtendI8x16(r, false, true)
	case 139: // i16x8.shl
		return f.i16x8Shift(r, f.a.NeonUshlH, false)
	case 140: // i16x8.shr_s
		return f.i16x8Shift(r, f.a.NeonSshrvH, true)
	case 141: // i16x8.shr_u
		return f.i16x8Shift(r, f.a.NeonUshrvH, true)
	case 142: // i16x8.add
		return f.v128Bin(r, f.a.NeonAddH)
	case 143: // i16x8.add_sat_s
		return f.v128Bin(r, f.a.NeonSqaddH)
	case 144: // i16x8.add_sat_u
		return f.v128Bin(r, f.a.NeonUqaddH)
	case 145: // i16x8.sub
		return f.v128Bin(r, f.a.NeonSubH)
	case 146: // i16x8.sub_sat_s
		return f.v128Bin(r, f.a.NeonSqsubH)
	case 147: // i16x8.sub_sat_u
		return f.v128Bin(r, f.a.NeonUqsubH)
	case 148: // f64x2.nearest
		return f.v128FloatRound(r, true, roundNearest)
	case 149: // i16x8.mul
		return f.v128Bin(r, f.a.NeonMulH)
	case 150: // i16x8.min_s
		return f.v128Bin(r, f.a.NeonSminH)
	case 151: // i16x8.min_u
		return f.v128Bin(r, f.a.NeonUminH)
	case 152: // i16x8.max_s
		return f.v128Bin(r, f.a.NeonSmaxH)
	case 153: // i16x8.max_u
		return f.v128Bin(r, f.a.NeonUmaxH)
	case 155: // i16x8.avgr_u
		return f.v128Bin(r, f.a.NeonUrhaddH)
	case 156: // i16x8.extmul_low_i8x16_s
		return f.i16x8ExtmulI8x16(r, true, false)
	case 157: // i16x8.extmul_high_i8x16_s
		return f.i16x8ExtmulI8x16(r, true, true)
	case 158: // i16x8.extmul_low_i8x16_u
		return f.i16x8ExtmulI8x16(r, false, false)
	case 159: // i16x8.extmul_high_i8x16_u
		return f.i16x8ExtmulI8x16(r, false, true)
	case 167: // i32x4.extend_low_i16x8_s
		return f.i32x4ExtendI16x8(r, true, false)
	case 168: // i32x4.extend_high_i16x8_s
		return f.i32x4ExtendI16x8(r, true, true)
	case 169: // i32x4.extend_low_i16x8_u
		return f.i32x4ExtendI16x8(r, false, false)
	case 170: // i32x4.extend_high_i16x8_u
		return f.i32x4ExtendI16x8(r, false, true)
	case 171: // i32x4.shl
		return f.i32x4Shift(r, f.a.NeonUshlS, false)
	case 172: // i32x4.shr_s
		return f.i32x4Shift(r, f.a.NeonSshrvS, true)
	case 173: // i32x4.shr_u
		return f.i32x4Shift(r, f.a.NeonUshrvS, true)
	case 199: // i64x2.extend_low_i32x4_s
		return f.i64x2ExtendI32x4(r, true, false)
	case 200: // i64x2.extend_high_i32x4_s
		return f.i64x2ExtendI32x4(r, true, true)
	case 201: // i64x2.extend_low_i32x4_u
		return f.i64x2ExtendI32x4(r, false, false)
	case 202: // i64x2.extend_high_i32x4_u
		return f.i64x2ExtendI32x4(r, false, true)
	case 203: // i64x2.shl
		return f.i64x2Shift(r, f.a.NeonUshlD, false)
	case 204: // i64x2.shr_s
		return f.i64x2Shift(r, f.a.NeonSshrvD, true)
	case 205: // i64x2.shr_u
		return f.i64x2Shift(r, f.a.NeonUshrvD, true)
	case 174: // i32x4.add
		return f.v128Bin(r, f.a.NeonAddS)
	case 177: // i32x4.sub
		return f.v128Bin(r, f.a.NeonSubS)
	case 181: // i32x4.mul
		return f.v128Bin(r, f.a.NeonMulS)
	case 182: // i32x4.min_s
		return f.v128Bin(r, f.a.NeonSminS)
	case 183: // i32x4.min_u
		return f.v128Bin(r, f.a.NeonUminS)
	case 184: // i32x4.max_s
		return f.v128Bin(r, f.a.NeonSmaxS)
	case 185: // i32x4.max_u
		return f.v128Bin(r, f.a.NeonUmaxS)
	case 186: // i32x4.dot_i16x8_s
		return f.i32x4DotI16x8S(r)
	case 188: // i32x4.extmul_low_i16x8_s
		return f.i32x4ExtmulI16x8(r, true, false)
	case 189: // i32x4.extmul_high_i16x8_s
		return f.i32x4ExtmulI16x8(r, true, true)
	case 190: // i32x4.extmul_low_i16x8_u
		return f.i32x4ExtmulI16x8(r, false, false)
	case 191: // i32x4.extmul_high_i16x8_u
		return f.i32x4ExtmulI16x8(r, false, true)
	case 206: // i64x2.add
		return f.v128Bin(r, f.a.NeonAddD)
	case 209: // i64x2.sub
		return f.v128Bin(r, f.a.NeonSubD)
	case 213: // i64x2.mul
		return f.i64x2Mul(r)
	case 220: // i64x2.extmul_low_i32x4_s
		f.i64x2ExtmulI32x4(true, false)
	case 221: // i64x2.extmul_high_i32x4_s
		f.i64x2ExtmulI32x4(true, true)
	case 222: // i64x2.extmul_low_i32x4_u
		f.i64x2ExtmulI32x4(false, false)
	case 223: // i64x2.extmul_high_i32x4_u
		f.i64x2ExtmulI32x4(false, true)
	case 214: // i64x2.eq
		return f.v128Bin(r, f.a.NeonCmeqD)
	case 215: // i64x2.ne
		return f.v128BinNot(r, f.a.NeonCmeqD)
	case 216: // i64x2.lt_s
		return f.i64x2SignedCmp(r, condL)
	case 217: // i64x2.gt_s
		return f.i64x2SignedCmp(r, condG)
	case 218: // i64x2.le_s
		return f.i64x2SignedCmp(r, condLE)
	case 219: // i64x2.ge_s
		return f.i64x2SignedCmp(r, condGE)
	case 224: // f32x4.abs
		return f.v128IntegerAbs(r, func(dst, src Reg) { f.a.NeonFabs(dst, src, false) })
	case 225: // f32x4.neg
		return f.v128IntegerAbs(r, func(dst, src Reg) { f.a.NeonFneg(dst, src, false) })
	case 227: // f32x4.sqrt
		return f.v128IntegerAbs(r, func(dst, src Reg) { f.a.NeonFsqrt(dst, src, false) })
	case 228: // f32x4.add
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFadd(dst, s1, s2, false) })
	case 229: // f32x4.sub
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFsub(dst, s1, s2, false) })
	case 230: // f32x4.mul
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFmul(dst, s1, s2, false) })
	case 231: // f32x4.div
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFdiv(dst, s1, s2, false) })
	case 232: // f32x4.min
		return f.v128FloatMinMax(r, false, false)
	case 233: // f32x4.max
		return f.v128FloatMinMax(r, false, true)
	case 234: // f32x4.pmin: deterministic pseudo-min with first operand winning equal/NaN-second lanes.
		return f.v128FloatPMinMax(r, false, false)
	case 235: // f32x4.pmax: deterministic pseudo-max with first operand winning equal/NaN-second lanes.
		return f.v128FloatPMinMax(r, false, true)
	case 236: // f64x2.abs
		return f.v128IntegerAbs(r, func(dst, src Reg) { f.a.NeonFabs(dst, src, true) })
	case 237: // f64x2.neg
		return f.v128IntegerAbs(r, func(dst, src Reg) { f.a.NeonFneg(dst, src, true) })
	case 239: // f64x2.sqrt
		return f.v128IntegerAbs(r, func(dst, src Reg) { f.a.NeonFsqrt(dst, src, true) })
	case 240: // f64x2.add
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFadd(dst, s1, s2, true) })
	case 241: // f64x2.sub
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFsub(dst, s1, s2, true) })
	case 242: // f64x2.mul
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFmul(dst, s1, s2, true) })
	case 243: // f64x2.div
		return f.v128Bin(r, func(dst, s1, s2 Reg) { f.a.NeonFdiv(dst, s1, s2, true) })
	case 244: // f64x2.min
		return f.v128FloatMinMax(r, true, false)
	case 245: // f64x2.max
		return f.v128FloatMinMax(r, true, true)
	case 246: // f64x2.pmin: deterministic pseudo-min with first operand winning equal/NaN-second lanes.
		return f.v128FloatPMinMax(r, true, false)
	case 247: // f64x2.pmax: deterministic pseudo-max with first operand winning equal/NaN-second lanes.
		return f.v128FloatPMinMax(r, true, true)
	case 248: // i32x4.trunc_sat_f32x4_s
		return f.v128I32x4TruncSat(r, false, true)
	case 249: // i32x4.trunc_sat_f32x4_u
		return f.v128I32x4TruncSat(r, false, false)
	case 250: // f32x4.convert_i32x4_s
		return f.v128I32x4ConvertToFloat(r, false, true)
	case 251: // f32x4.convert_i32x4_u
		return f.v128I32x4ConvertToFloat(r, false, false)
	case 252: // i32x4.trunc_sat_f64x2_s_zero
		return f.v128I32x4TruncSat(r, true, true)
	case 253: // i32x4.trunc_sat_f64x2_u_zero
		return f.v128I32x4TruncSat(r, true, false)
	case 254: // f64x2.convert_low_i32x4_s
		return f.v128I32x4ConvertToFloat(r, true, true)
	case 255: // f64x2.convert_low_i32x4_u
		return f.v128I32x4ConvertToFloat(r, true, false)
	case 83: // v128.any_true
		f.v128AnyTrue()
	case 94: // f32x4.demote_f64x2_zero
		return f.v128DemoteF64x2Zero(r)
	case 95: // f64x2.promote_low_f32x4
		return f.v128PromoteLowF32x4(r)
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
		return f.v128IntegerAbs(r, f.a.NeonAbsB)
	case 97: // i8x16.neg
		return f.v128IntegerNeg(r, f.a.NeonNegB)
	case 98: // i8x16.popcnt
		return f.i8x16Popcnt(r)
	case 128: // i16x8.abs
		return f.v128IntegerAbs(r, f.a.NeonAbsH)
	case 129: // i16x8.neg
		return f.v128IntegerNeg(r, f.a.NeonNegH)
	case 160: // i32x4.abs
		return f.v128IntegerAbs(r, f.a.NeonAbsS)
	case 161: // i32x4.neg
		return f.v128IntegerNeg(r, f.a.NeonNegS)
	case 192: // i64x2.abs
		return f.i64x2Abs(r)
	case 193: // i64x2.neg
		return f.v128IntegerNeg(r, f.a.NeonNegD)
	case 77: // v128.not
		return f.v128UnaryNot(r)
	case 78: // v128.and
		return f.v128Bin(r, f.a.NeonAnd16b)
	case 79: // v128.andnot (a &^ b)
		// NEON BIC Vd.16b, Vn.16b, Vm.16b computes Vn & ~Vm directly, so Wasm
		// andnot(a,b) = a & ~b is a single BIC dst,a,b. As an op(dst,s1,s2) closure
		// it sinks in place through v128Bin like the other binary ops.
		return f.v128Bin(r, f.a.NeonAndn16b)
	case 80: // v128.or
		return f.v128Bin(r, f.a.NeonOrr16b)
	case 81: // v128.xor
		return f.v128Bin(r, f.a.NeonEor16b)
	case 82: // v128.bitselect: (a & mask) | (b & ~mask)
		f.v128Bitselect()
	default:
		return fmt.Errorf("arm64: unsupported 0xFD opcode %d", sub)
	}
	return nil
}
