//go:build riscv64

package riscv64

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// floatBits returns the bit pattern of v in the given float width.
func floatBits(v float64, f64 bool) uint64 {
	if f64 {
		return math.Float64bits(v)
	}
	return uint64(math.Float32bits(float32(v)))
}

// Floating point (f32/f64) uses the scalar F/D register file with NaN- and
// signed-zero-correct sequences. Operands are handled eagerly by a parallel
// floating-register allocator (fregUser/fpinned); integer and floating register
// namespaces remain independent.

// --- floating-register allocator ---

func (f *fn) occupyF(e *elem, r Reg) {
	f.fregUser[r] = e
	if e.kind == ekDeferred && e.typ != mtNone {
		e.st.typ = e.typ
	}
	e.kind = ekValue
	e.st.kind, e.st.reg = stReg, r
}

func (f *fn) releaseF(r Reg) {
	if r != regNone {
		f.fregUser[r] = nil
	}
}

type floatConstReg struct {
	typ  machineType
	bits int64
	reg  Reg
}

func (f *fn) fconstMask() regMask {
	var m regMask
	for _, c := range f.fconsts {
		m = m.add(c.reg)
	}
	return m
}

// allocFReg returns a free scalar FP register, spilling the deepest
// float-resident stack value if none is free.
func (f *fn) allocFReg(avoid regMask) Reg {
	block := avoid.union(f.fpinned).union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	for _, r := range fpAllocRegs {
		if f.fregUser[r] == nil && !block.has(r) {
			return r
		}
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue && e.st.kind == stReg && e.st.typ.isXMM() && !block.has(e.st.reg) {
			r := e.st.reg
			f.spillF(e)
			return r
		}
	}
	panic("riscv64: no V register available to spill")
}

// spillF evicts a scalar FP value to a fresh frame slot.
func (f *fn) spillF(e *elem) {
	r := e.st.reg
	slot := f.allocSpillSlot()
	f.a.StrF(SP, f.spillOff(slot), r, true)
	f.fregUser[r] = nil
	f.replaceStorage(e, storage{kind: stSlot, typ: e.st.typ, slot: slot})
}

// materializeF ensures float value e lives in a V register and returns it.
func (f *fn) materializeF(e *elem) Reg {
	switch e.st.kind {
	case stReg:
		return e.st.reg
	case stConst:
		if !f.usesCalls {
			if c, ok := f.floatConstReg(e.st); ok {
				x := f.allocFReg(maskOf(c))
				f.a.FmovReg(x, c, e.st.typ == mtF64)
				f.occupyF(e, x)
				return x
			}
		}
		x := f.allocFReg(0)
		f.loadFConst(x, e.st)
		f.occupyF(e, x)
		return x
	case stSlot:
		x := f.allocFReg(0)
		f.a.LdrF(x, SP, f.spillOff(e.st.slot), true) // 8B; f32 uses the low 4
		f.occupyF(e, x)
		return x
	case stLocalRef:
		x := f.allocFReg(0)
		f.a.LdrF(x, SP, f.localOff(e.st.idx), e.st.typ == mtF64)
		f.occupyF(e, x)
		return x
	case stLocalReg:
		// Borrowed pinned float local: copy into an owned V register so the caller
		// may clobber it without corrupting the local.
		x := f.allocFReg(0)
		f.a.FmovReg(x, e.st.reg, e.st.typ == mtF64)
		f.occupyF(e, x)
		return x
	case stMemRef:
		x := f.allocFReg(0)
		f.loadFMemRef(x, e.st)
		f.releaseMemRef(e.st)
		f.occupyF(e, x)
		return x
	}
	panic("riscv64: cannot materialize float storage")
}

// operandRegF returns a floating register holding e's value for READ-ONLY use
// source operand (never written, so it need not be a private copy). A pinned float
// local is used directly and must not be released (owned=false); everything else is
// materialized into an owned scratch register the caller releases (owned=true).
// This avoids the fmov-to-scratch that materializeF emits for a pinned local when
// the value is only being read — the dominant per-op float overhead.
func (f *fn) operandRegF(e *elem) (reg Reg, owned bool) {
	if e.kind == ekValue && e.st.kind == stLocalReg {
		return e.st.reg, false
	}
	if e.kind == ekValue && e.st.kind == stConst && e.st.typ.isFloat() && !f.usesCalls {
		if r, ok := f.floatConstReg(e.st); ok {
			return r, false
		}
	}
	return f.materializeF(e), true
}

func (f *fn) floatConstReg(st storage) (Reg, bool) {
	for _, c := range f.fconsts {
		if c.typ == st.typ && c.bits == st.cval {
			return c.reg, true
		}
	}
	if len(f.fconsts) >= 2 {
		return regNone, false
	}
	x := f.allocFReg(0)
	f.loadFConst(x, st)
	f.fconsts = append(f.fconsts, floatConstReg{typ: st.typ, bits: st.cval, reg: x})
	return x, true
}

func (f *fn) preloadFloatConsts(code []byte) {
	if f.usesCalls {
		return
	}
	r := wasm.NewReader(code)
	for r.HasNext() && len(f.fconsts) < 2 {
		op, err := r.Byte()
		if err != nil {
			return
		}
		switch op {
		case 0x43: // f32.const
			bits, err := r.LEU32()
			if err != nil {
				return
			}
			f.floatConstReg(storage{kind: stConst, typ: mtF32, cval: int64(bits)})
		case 0x44: // f64.const
			bits, err := r.LEU64()
			if err != nil {
				return
			}
			f.floatConstReg(storage{kind: stConst, typ: mtF64, cval: int64(bits)})
		default:
			if err := wasm.SkipInstructionImmediate(r, op); err != nil {
				return
			}
		}
	}
}

// pushFReg pushes a V-resident float value of the given type.
func (f *fn) pushFReg(r Reg, typ machineType) *elem {
	e := f.pushValue(storage{kind: stReg, typ: typ, reg: r})
	f.fregUser[r] = e
	return e
}

// loadFConst materializes a float constant's bits into V register r (via a GP
// scratch).
func (f *fn) loadFConst(r Reg, st storage) {
	t := f.allocReg(0)
	if st.typ == mtF64 {
		f.a.MovImm64(t, uint64(st.cval))
		f.a.FmovFromGpr(r, t, true)
	} else {
		f.a.MovImm32(t, int32(uint32(st.cval)))
		f.a.FmovFromGpr(r, t, false)
	}
	f.release(t)
}

// Rounding modes for FRINT* (nearest/floor/ceil/trunc), matching wasm's
// non-trapping rounding. The encoder maps these selectors to
// FRINTN/FRINTM/FRINTP/FRINTZ.
const (
	roundNearest byte = 'n'
	roundFloor   byte = 'm'
	roundCeil    byte = 'p'
	roundTrunc   byte = 'z'
)

// --- float op handlers ---

func (f *fn) fconst(bits uint64, typ machineType) {
	f.pushValue(storage{kind: stConst, typ: typ, cval: int64(bits)})
}

// fbin lowers add/sub/mul/div via the 3-operand form dst = s1 <op> s2. Both
// operands are read directly (a pinned local is borrowed, never copied), and the
// result lands in a reused owned-operand register or a fresh one — so no operand is
// pre-copied to scratch.
//
// riscv64 has no memory-source float ops (§4a): a stMemRef right operand is not
// folded here; operandRegF materializes it with an explicit LDR (loadFMemRef).
// memOp is retained for caller-signature parity with the amd64 twin and is unused.
func (f *fn) fbin(vop func(dst, s1, s2 Reg, f64 bool), memOp byte, f64 bool) {
	b := f.popValue()
	a := f.popValue()
	s1, o1 := f.operandRegF(a)
	f.fpinned = f.fpinned.add(s1)
	s2, o2 := f.operandRegF(b)
	// Destination: reuse an owned operand's register in place (it is being
	// consumed), else a fresh register so a borrowed pinned local isn't clobbered.
	var dst Reg
	switch {
	case o1:
		dst = s1
	case o2:
		dst = s2
	default:
		// Both operands are borrowed pinned locals (blocked from allocation via the
		// pinned-local mask); s1 is also fpinned here, so a fresh dst avoids both.
		dst = f.allocFReg(0)
	}
	f.fpinned = f.fpinned.remove(s1)
	vop(dst, s1, s2, f64)
	if o1 && dst != s1 {
		f.releaseF(s1)
	}
	if o2 && dst != s2 {
		f.releaseF(s2)
	}
	f.pushFReg(dst, mtOf2(f64))
}

func (f *fn) fbinInto(dst Reg, vop func(dst, s1, s2 Reg, f64 bool), memOp byte, f64 bool) {
	b := f.popValue()
	a := f.popValue()
	s1, o1 := f.operandRegF(a)
	f.fpinned = f.fpinned.add(s1)
	s2, o2 := f.operandRegF(b)
	f.fpinned = f.fpinned.remove(s1)
	vop(dst, s1, s2, f64)
	if o1 && dst != s1 {
		f.releaseF(s1)
	}
	if o2 && dst != s2 {
		f.releaseF(s2)
	}
}

// scalarFMinMaxInto implements wasm min/max for one scalar lane. Branch on the
// ordered compare; equal uses bitwise zero fixups, distinct ordered operands use
// scalar FMIN/FMAX like wazero, and unordered propagates a quiet NaN through scalar
// add.
func (f *fn) scalarFMinMaxInto(xa, xb Reg, f64, isMax bool) {
	f.a.Fcmp(xa, xb, f64)
	jnan := f.a.Bcond(condVS)  // unordered (NaN): riscv64 FCMP sets V on unordered
	jdist := f.a.Bcond(condNE) // distinct ordered operands

	// Equal (incl. ±0): combine the scalar encodings so max(-0,+0)=+0
	// (AND) and min(+0,-0)=-0 (OR). The typed helpers preserve RV64's required
	// NaN-boxing for f32 values.
	if isMax {
		f.a.FandBits(xa, xa, xb, f64)
	} else {
		f.a.ForBits(xa, xa, xb, f64)
	}
	jdone := f.a.Branch()

	f.a.PatchBranch19(jdist, f.a.Len())
	// Distinct ordered operands: scalar FMAX/FMIN give the larger/smaller, matching
	// wazero (the operands are neither NaN nor equal here).
	if isMax {
		f.a.Fmax(xa, xa, xb, f64)
	} else {
		f.a.Fmin(xa, xa, xb, f64)
	}
	jdone2 := f.a.Branch()

	f.a.PatchBranch19(jnan, f.a.Len())
	f.a.Fadd(xa, xa, xb, f64) // NaN + x -> quiet NaN, matching wazero

	f.a.PatchBranch26(jdone, f.a.Len())
	f.a.PatchBranch26(jdone2, f.a.Len())
}

// fminmaxInto lowers scalar wasm min/max through the shared lane helper used by
// SIMD. When dst is supplied, it is a pinned local's V register and the result
// is sunk there directly; otherwise an owned operand register is reused.
func (f *fn) fminmaxInto(dst Reg, f64, isMax bool) {
	b := f.popValue()
	a := f.popValue()
	xa, xaOwned := f.operandRegF(a)
	f.fpinned = f.fpinned.add(xa)
	xb, xbOwned := f.operandRegF(b) // read-only: compared and combined into xa
	f.fpinned = f.fpinned.remove(xa)
	if dst == regNone {
		if xaOwned {
			dst = xa
		} else {
			dst = f.allocFReg(maskOf(xa, xb))
		}
	}
	if dst != xa {
		f.a.FmovReg(dst, xa, f64)
	}
	f.scalarFMinMaxInto(dst, xb, f64, isMax)
	if xaOwned && dst != xa {
		f.releaseF(xa)
	}
	if xbOwned {
		f.releaseF(xb)
	}
	f.pushFReg(dst, mtOf2(f64))
}

func (f *fn) fminmax(f64, isMax bool) {
	f.fminmaxInto(regNone, f64, isMax)
}

func (f *fn) fsqrt(f64 bool) {
	src, owned := f.operandRegF(f.popValue())
	dst := src
	if !owned { // borrowed pinned local: write a fresh dest, leave the local intact
		dst = f.allocFReg(maskOf(src))
	}
	f.a.Fsqrt(dst, src, f64)
	f.pushFReg(dst, mtOf2(f64))
}

// fsign applies an IEEE-754 sign operation with the native FSGNJ family. Unlike
// integer bit-twiddling through a 64-bit move, these instructions preserve f32
// NaN-boxing and retain NaN payload bits exactly.
func (f *fn) fsign(neg, f64 bool) {
	src, owned := f.operandRegF(f.popValue())
	dst := src
	if !owned {
		dst = f.allocFReg(maskOf(src))
	}
	if neg {
		f.a.Fneg(dst, src, f64)
	} else {
		f.a.Fabs(dst, src, f64)
	}
	f.pushFReg(dst, mtOf2(f64))
}

func (f *fn) fneg(f64 bool) { f.fsign(true, f64) }
func (f *fn) fabs(f64 bool) { f.fsign(false, f64) }

func (f *fn) fround(f64 bool, mode byte) {
	src, owned := f.operandRegF(f.popValue())
	dst := src
	if !owned { // borrowed pinned local: round into a fresh dest, leave the local intact
		dst = f.allocFReg(maskOf(src))
	}
	f.a.Frint(dst, src, f64, mode)
	f.pushFReg(dst, mtOf2(f64))
}

func (f *fn) fcopysign(f64 bool) {
	b := f.popValue()
	a := f.popValue()
	xa, xaOwned := f.operandRegF(a)
	f.fpinned = f.fpinned.add(xa)
	xb, xbOwned := f.operandRegF(b)
	f.fpinned = f.fpinned.remove(xa)
	dst := xa
	if !xaOwned {
		dst = f.allocFReg(maskOf(xa, xb))
	}
	f.a.Fcopysign(dst, xa, xb, f64)
	if xaOwned && dst != xa {
		f.releaseF(xa)
	}
	if xbOwned {
		f.releaseF(xb)
	}
	f.pushFReg(dst, mtOf2(f64))
}

// fcmp lowers a NaN-correct float comparison to a 0/1 i32 result. riscv64 FCMP sets
// NZCV with a defined unordered result (V set, Z clear), so each wasm float compare
// lowers directly to FCMP + Cset with the float condition (§4b) — no parity dance.
func (f *fn) fcmp(kind wOp, f64 bool) {
	b := f.popValue()
	a := f.popValue()
	xa, xaOwned := f.operandRegF(a) // read-only: only compared
	f.fpinned = f.fpinned.add(xa)
	xb, xbOwned := f.operandRegF(b) // read-only: only compared
	f.fpinned = f.fpinned.remove(xa)
	dst := f.allocReg(0)
	f.emitFCmpCset(kind, xa, xb, f64, dst)
	if xaOwned {
		f.releaseF(xa)
	}
	if xbOwned {
		f.releaseF(xb)
	}
	f.pushReg(dst, mtI32)
}

// emitFCmpCset emits FCMP plus the NaN-correct CSET for a float relational op,
// landing a 0/1 i32 boolean in dst. The ordered ops (gt/ge/lt/le) use GT/GE
// (unordered clears N=V, so NaN yields false); lt/le swap the operands. Shared by
// fcmp (eager boolean) and condenseFCompareValue (deferred-node fallback).
func (f *fn) emitFCmpCset(kind wOp, xa, xb Reg, f64 bool, dst Reg) {
	switch kind {
	case opEq: // ordered equal: EQ requires Z=1, which unordered does not set
		f.a.Fcmp(xa, xb, f64)
		f.a.Cset32(dst, condE)
	case opNe: // not-equal or unordered: NE (Z=0) is set on both
		f.a.Fcmp(xa, xb, f64)
		f.a.Cset32(dst, condNE)
	case opGtS: // fc gt
		f.a.Fcmp(xa, xb, f64)
		f.a.Cset32(dst, condG)
	case opGeS: // fc ge
		f.a.Fcmp(xa, xb, f64)
		f.a.Cset32(dst, condGE)
	case opLtS: // a<b == b>a
		f.a.Fcmp(xb, xa, f64)
		f.a.Cset32(dst, condG)
	case opLeS: // a<=b == b>=a
		f.a.Fcmp(xb, xa, f64)
		f.a.Cset32(dst, condGE)
	}
}

// pushFCompare pushes a DEFERRED float relational op (gt/ge/lt/le only) instead
// of materializing a boolean, so the immediately-following if/br_if can fuse it
// into FCMP + B.cond via condenseFCompareToFlags. The driver only defers when the
// next opcode is if/br_if, so the node never lingers past its consumer. eq/ne are
// never deferred (their branch form needs two conditional branches).
func (f *fn) pushFCompare(op wOp, f64 bool) {
	typ := mtF32
	if f64 {
		typ = mtF64
	}
	right := f.s.back()
	left := baseOfValentBlock(right).prev
	node := f.s.alloc()
	node.kind, node.op, node.typ = ekDeferred, op, typ
	node.arg0, node.arg1 = left, right
	node.deferDepth = 1 + max16(deferDepthOf(left), deferDepthOf(right))
	f.s.push(node)
}

// condenseFCompareToFlags lowers a deferred float relational node to FCMP (no
// CSET), consumes the node and its operands, and returns the branch condition
// that is true when the comparison holds. Mirrors emitFCmpCset's operand
// ordering. invert (from an eqz peel) flips the condition; that stays NaN-correct
// because wasm's eqz(float-cmp) and the inverted riscv64 condition both include the
// unordered case on the negated side (GT↔LE, GE↔LT).
func (f *fn) condenseFCompareToFlags(node *elem, invert bool) Cond {
	f.stats.peep("fcmp-branch-fuse")
	f64 := node.typ == mtF64
	xa, xaOwned := f.operandRegF(node.arg0)
	f.fpinned = f.fpinned.add(xa)
	xb, xbOwned := f.operandRegF(node.arg1)
	f.fpinned = f.fpinned.remove(xa)
	var cc Cond
	switch node.op {
	case opGtS:
		f.a.Fcmp(xa, xb, f64)
		cc = condG
	case opGeS:
		f.a.Fcmp(xa, xb, f64)
		cc = condGE
	case opLtS:
		f.a.Fcmp(xb, xa, f64)
		cc = condG
	case opLeS:
		f.a.Fcmp(xb, xa, f64)
		cc = condGE
	}
	if xaOwned {
		f.releaseF(xa)
	}
	if xbOwned {
		f.releaseF(xb)
	}
	if invert {
		cc = invertCond(cc)
	}
	f.consumeBlockBelow(node)
	f.erase(node)
	return cc
}

// condenseFCompareValue materializes a deferred float relational node to a 0/1
// boolean. Defensive: the driver only defers a float compare directly before its
// if/br_if consumer, so this is normally unreachable, but it keeps a deferred
// float node correct on any path that condenses it as a value.
func (f *fn) condenseFCompareValue(node *elem, dest Reg) Reg {
	f.stats.peep("fcmp-value-fallback")
	f64 := node.typ == mtF64
	xa, xaOwned := f.operandRegF(node.arg0)
	f.fpinned = f.fpinned.add(xa)
	xb, xbOwned := f.operandRegF(node.arg1)
	f.fpinned = f.fpinned.remove(xa)
	result := dest
	if result == regNone {
		result = f.allocReg(0)
	}
	f.emitFCmpCset(node.op, xa, xb, f64, result)
	if xaOwned {
		f.releaseF(xa)
	}
	if xbOwned {
		f.releaseF(xb)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.st.typ = mtI32
	node.op = opNone
	return result
}

// i2f converts a signed integer to float. srcWide selects an i64 source.
func (f *fn) i2f(f64, srcWide bool) {
	gpr := f.materialize(f.popValue())
	xmm := f.allocFReg(0)
	f.a.Scvtf(xmm, gpr, f64, srcWide)
	f.release(gpr)
	f.pushFReg(xmm, mtOf2(f64))
}

// i2fU converts an unsigned integer directly with RV64 UCVTF. Besides being
// shorter than the old branch-and-bias sequence, the native instruction handles
// the full u64 range and IEEE rounding without relying on width-flag conventions.
func (f *fn) i2fU(f64, srcWide bool) {
	gpr := f.materialize(f.popValue())
	xmm := f.allocFReg(0)
	f.a.Ucvtf(xmm, gpr, f64, srcWide)
	f.release(gpr)
	f.pushFReg(xmm, mtOf2(f64))
}

// truncLimitBits returns the exclusive source-width float bounds outside which a
// trunc to the given integer type must trap (x valid iff min < x < max). Mirrors
// WARP FloatTruncLimitsExcl.
func truncLimitBits(signed, f64src, dstWide bool) (minBits, maxBits uint64) {
	switch {
	case !f64src && signed && !dstWide:
		return 0xCF000001, 0x4F000000
	case !f64src && signed && dstWide:
		return 0xDF000001, 0x5F000000
	case !f64src && !signed && !dstWide:
		return 0xBF800000, 0x4F800000
	case !f64src && !signed && dstWide:
		return 0xBF800000, 0x5F800000
	case f64src && signed && !dstWide:
		return 0xC1E0000000200000, 0x41E0000000000000
	case f64src && signed && dstWide:
		return 0xC3E0000000000001, 0x43E0000000000000
	case f64src && !signed && !dstWide:
		return 0xBFF0000000000000, 0x41F0000000000000
	default:
		return 0xBFF0000000000000, 0x43F0000000000000
	}
}

// loadFConstBits materializes raw float bits into a fresh V register.
func (f *fn) loadFConstBits(bits uint64, f64 bool) Reg {
	x := f.allocFReg(0)
	f.loadFConst(x, storage{typ: mtOf2(f64), cval: int64(bits)})
	return x
}

// f2iTrunc converts float→int with truncation, trapping (TruncOverflow) on NaN or
// out-of-range. srcF64 selects the source width; dstWide the i64 destination.
func (f *fn) f2iTrunc(dstWide, srcF64, signed bool) {
	x := f.materializeF(f.popValue())
	f.fpinned = f.fpinned.add(x)

	minBits, maxBits := truncLimitBits(signed, srcF64, dstWide)
	f.a.Fcmp(x, x, srcF64)
	f.trapIf(condVS, trapTruncOverflow) // NaN (unordered)
	lo := f.loadFConstBits(minBits, srcF64)
	f.a.Fcmp(x, lo, srcF64)
	f.releaseF(lo)
	f.trapIf(condLE, trapTruncOverflow) // x <= lower-exclusive limit
	hi := f.loadFConstBits(maxBits, srcF64)
	f.a.Fcmp(x, hi, srcF64)
	f.releaseF(hi)
	f.trapIf(condGE, trapTruncOverflow) // x >= upper-exclusive limit

	r := f.allocReg(0)
	switch {
	case signed:
		f.a.Fcvtzs(r, x, srcF64, dstWide)
	case !dstWide: // u32: a 64-bit signed cvt is exact on [0, 2^32)
		f.a.Fcvtzs(r, x, srcF64, true)
	default: // u64
		f.truncU64InRange(x, r, srcF64)
	}
	f.fpinned = f.fpinned.remove(x)
	f.releaseF(x)
	f.pushReg(r, mtOfInt(dstWide))
}

// truncU64InRange converts x, already proven in [0, 2^64), to u64: a signed cvt
// overflows for x >= 2^63, so bias by cvt(x - 2^63) + 2^63.
func (f *fn) truncU64InRange(x, r Reg, srcF64 bool) {
	p63 := f.loadFConstBits(floatBits2p63(srcF64), srcF64)
	f.a.Fcmp(x, p63, srcF64)
	simple := f.a.Bcond(condL) // x < 2^63 (ordered)
	f.a.Fsub(x, x, p63, srcF64)
	f.a.Fcvtzs(r, x, srcF64, true)
	t := f.allocReg(maskOf(r))
	f.a.MovImm64(t, 0x8000000000000000)
	f.a.Add64(r, r, t)
	f.release(t)
	done := f.a.Branch()
	f.a.PatchBranch19(simple, f.a.Len())
	f.a.Fcvtzs(r, x, srcF64, true)
	f.a.PatchBranch26(done, f.a.Len())
	f.releaseF(p63)
}

// floatBits2p63 returns the bit pattern of 2^63 in the given float width.
func floatBits2p63(f64 bool) uint64 { return floatBits(math.Ldexp(1, 63), f64) }

// --- saturating float→int truncation (0xFC 0-7): NaN→0, out-of-range clamps ---

func (f *fn) truncSat(f64src, dstWide, signed bool) {
	x := f.materializeF(f.popValue())
	f.fpinned = f.fpinned.add(x)
	r := f.allocReg(0)
	f.pinned = f.pinned.add(r)
	switch {
	case signed:
		f.truncSatSigned(x, r, f64src, dstWide)
	case dstWide:
		f.truncSatU64(x, r, f64src)
	default:
		f.truncSatU32(x, r, f64src)
	}
	f.pinned = f.pinned.remove(r)
	f.fpinned = f.fpinned.remove(x)
	f.releaseF(x)
	f.pushReg(r, mtOfInt(dstWide))
}

func (f *fn) truncSatSigned(x, r Reg, f64src, dstWide bool) {
	n := 32
	if dstWide {
		n = 64
	}
	f.a.Fcvtzs(r, x, f64src, dstWide)
	f.a.Fcmp(x, x, f64src)
	notNaN := f.a.Bcond(condVC) // ordered
	f.a.MovImm64(r, 0)          // NaN → 0
	toEnd := f.a.Branch()
	f.a.PatchBranch19(notNaN, f.a.Len())
	hi := f.loadFConstBits(floatBits(math.Ldexp(1, n-1), f64src), f64src) // 2^(n-1)
	f.a.Fcmp(x, hi, f64src)
	f.releaseF(hi)
	below := f.a.Bcond(condL) // x < 2^(n-1) (ordered; NaN excluded above)
	if dstWide {
		f.a.MovImm64(r, 0x7FFFFFFFFFFFFFFF)
	} else {
		f.a.MovImm32(r, 0x7FFFFFFF)
	}
	f.a.PatchBranch19(below, f.a.Len())
	f.a.PatchBranch26(toEnd, f.a.Len())
}

func (f *fn) truncSatU32(x, r Reg, f64src bool) {
	f.a.Fcvtzs(r, x, f64src, true) // i64 trunc; low 32 is the in-range u32
	zero := f.loadFConstBits(floatBits(0, f64src), f64src)
	f.a.Fcmp(x, zero, f64src)
	f.releaseF(zero)
	pos := f.a.Bcond(condG) // x > 0 (ordered; NaN → not taken)
	f.a.MovImm64(r, 0)      // NaN/≤0 → 0
	toEnd := f.a.Branch()
	f.a.PatchBranch19(pos, f.a.Len())
	hi := f.loadFConstBits(floatBits(math.Ldexp(1, 32), f64src), f64src)
	f.a.Fcmp(x, hi, f64src)
	f.releaseF(hi)
	below := f.a.Bcond(condL)
	f.a.MovImm32(r, -1) // ≥2^32 → 0xFFFFFFFF
	f.a.PatchBranch19(below, f.a.Len())
	f.a.PatchBranch26(toEnd, f.a.Len())
}

func (f *fn) truncSatU64(x, r Reg, f64src bool) {
	zero := f.loadFConstBits(floatBits(0, f64src), f64src)
	f.a.Fcmp(x, zero, f64src)
	f.releaseF(zero)
	pos := f.a.Bcond(condG)
	f.a.MovImm64(r, 0)
	end0 := f.a.Branch()
	f.a.PatchBranch19(pos, f.a.Len())
	hi := f.loadFConstBits(floatBits(math.Ldexp(1, 64), f64src), f64src)
	f.a.Fcmp(x, hi, f64src)
	f.releaseF(hi)
	inRange := f.a.Bcond(condL)
	f.a.MovImm64(r, 0xFFFFFFFFFFFFFFFF) // ≥2^64 → all ones
	endMax := f.a.Branch()
	f.a.PatchBranch19(inRange, f.a.Len())
	p63 := f.loadFConstBits(floatBits2p63(f64src), f64src)
	f.a.Fcmp(x, p63, f64src)
	simple := f.a.Bcond(condL)
	f.a.Fsub(x, x, p63, f64src)
	f.a.Fcvtzs(r, x, f64src, true)
	t := f.allocReg(maskOf(r))
	f.a.MovImm64(t, 0x8000000000000000)
	f.a.Add64(r, r, t)
	f.release(t)
	biasEnd := f.a.Branch()
	f.a.PatchBranch19(simple, f.a.Len())
	f.a.Fcvtzs(r, x, f64src, true)
	f.a.PatchBranch26(biasEnd, f.a.Len())
	f.releaseF(p63)
	f.a.PatchBranch26(endMax, f.a.Len())
	f.a.PatchBranch26(end0, f.a.Len())
}

func (f *fn) fpromote() { // f32 → f64
	x := f.materializeF(f.popValue())
	f.a.FcvtS2D(x, x)
	f.pushFReg(x, mtF64)
}
func (f *fn) fdemote() { // f64 → f32
	x := f.materializeF(f.popValue())
	f.a.FcvtD2S(x, x)
	f.pushFReg(x, mtF32)
}

func (f *fn) reinterpretIntToFloat(wide bool) {
	gpr := f.materialize(f.popValue())
	xmm := f.allocFReg(0)
	f.a.FmovFromGpr(xmm, gpr, wide)
	f.release(gpr)
	f.pushFReg(xmm, mtOf2(wide))
}
func (f *fn) reinterpretFloatToInt(wide bool) {
	xmm := f.materializeF(f.popValue())
	gpr := f.allocReg(0)
	f.a.FmovToGpr(gpr, xmm, wide)
	f.releaseF(xmm)
	f.pushReg(gpr, mtOfInt(wide))
}

// fload / fstore reuse the integer bounds-checked effective-address path.
func (f *fn) fload(r *wasm.Reader, f64 bool) error {
	if _, err := r.U32(); err != nil {
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	size := 4
	if f64 {
		size = 8
	}
	ea, eaOwned, borrow, disp := f.memAddr(off, size, true)
	e := f.pushValue(fmemRefStorage(ea, disp, f64, borrow))
	if eaOwned {
		f.regUser[ea] = e
	}
	return nil
}

func (f *fn) fstore(r *wasm.Reader, f64 bool) error {
	if _, err := r.U32(); err != nil {
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	size := 4
	if f64 {
		size = 8
	}
	xmm := f.materializeF(f.popValue())
	f.fpinned = f.fpinned.add(xmm)
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	f.pinned = f.pinned.add(ea)
	f.materializePendingLoadsBeforeStore(ea, disp, size)
	f.a.StrFIdx(linMemReg, ea, xmm, disp, f64)
	f.pinned = f.pinned.remove(ea)
	f.fpinned = f.fpinned.remove(xmm)
	if eaOwned {
		f.release(ea)
	}
	f.releaseF(xmm)
	return nil
}

// helpers

func (f *fn) loadFMemRef(dst Reg, st storage) {
	f.a.LdrFIdx(dst, linMemReg, st.reg, st.memDisp(), st.typ == mtF64)
}

func fsize(f64 bool) int {
	if f64 {
		return 8
	}
	return 4
}

func mtOf2(f64 bool) machineType {
	if f64 {
		return mtF64
	}
	return mtF32
}
func mtOfInt(wide bool) machineType {
	if wide {
		return mtI64
	}
	return mtI32
}
