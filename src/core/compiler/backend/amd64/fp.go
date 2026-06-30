package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// xmm15 is reserved by the engine trampoline.
var fscratch = []Reg{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}

func (g *cg) freeFReg(r Reg) { g.fbusy[r] = false }

func (g *cg) allocFRegExcept(except Reg) Reg {
	for _, r := range fscratch {
		if r != except && !g.fbusy[r] {
			g.fbusy[r] = true
			return r
		}
	}
	for i := range g.st {
		if g.st[i].kind == vReg && g.st[i].fp && g.st[i].reg != except {
			r := g.st[i].reg
			g.a.FStoreDisp(RBP, g.slotOff(i), r, true)
			g.st[i] = ventry{kind: vSpill, fp: true, slot: i}
			g.fbusy[r] = true
			return r
		}
	}
	panic("amd64: no XMM register available to spill")
}

func (g *cg) allocFReg() Reg { return g.allocFRegExcept(0xFF) }

func (g *cg) loadFloatInto(dst Reg, e ventry) {
	switch e.kind {
	case vConst:
		if e.wide {
			g.a.MovImm64(RSI, uint64(e.cval))
			g.a.MovGprToXmm(dst, RSI, true)
		} else {
			g.a.MovImm32(RSI, int32(e.cval))
			g.a.MovGprToXmm(dst, RSI, false)
		}
	case vLocal:
		g.a.FLoadDisp(dst, RBP, g.localOff(e.local), true)
	case vReg:
		if e.reg != dst {
			g.a.FMov(dst, e.reg, true)
			g.freeFReg(e.reg)
		}
	case vSpill:
		g.a.FLoadDisp(dst, RBP, g.slotOff(e.slot), true)
	}
}

func (g *cg) materializeF(e ventry) Reg {
	if e.kind == vReg && e.fp {
		return e.reg
	}
	dst := g.allocFReg()
	g.loadFloatInto(dst, e)
	return dst
}

func (g *cg) pushFReg(r Reg) { g.fbusy[r] = true; g.push(ventry{kind: vReg, fp: true, reg: r}) }

func (g *cg) fbin(op func(dst, src Reg, f64 bool), f64 bool, kind fbinOp) {
	b := g.pop()
	a := g.pop()
	if a.kind == vConst && b.kind == vConst && a.fp && b.fp {
		if v, ok := foldFloatBin(kind, a, b, f64); ok {
			g.push(v)
			return
		}
	}
	dst := g.materializeF(a)
	src := g.materializeF(b)
	op(dst, src, f64)
	g.freeFReg(src)
	g.pushFReg(dst)
}

// fminmax lowers f32/f64 min/max with WebAssembly semantics, which x86
// minss/maxss get wrong on signed zeros and NaN (they return the second operand
// for equal values and for NaN). We branch on the ordered comparison:
//   - unordered (NaN input): propagate a quieted NaN via addss/addsd
//   - equal (incl. +0 vs -0): combine bitwise — OR for min keeps -0, AND for max
//     keeps +0; for equal non-zero values OR/AND is idempotent
//   - ordered and distinct: minss/maxss is already correct
func (g *cg) fminmax(f64, isMax bool) {
	b := g.pop()
	a := g.pop()
	xa := g.materializeF(a)
	xb := g.materializeF(b)
	g.a.Ucomis(xa, xb, f64)
	jnan := g.a.JccPlaceholder(CondP)   // unordered: a or b is NaN
	jdist := g.a.JccPlaceholder(CondNE) // a != b: ordered and distinct

	// Equal (including +0/-0): a sign-correct bitwise combine. orps/andps have no
	// SSE prefix for f32; pd variants use the 0x66 prefix.
	var prefix, bitOp byte
	if f64 {
		prefix = 0x66
	}
	if isMax {
		bitOp = 0x54 // andps/pd
	} else {
		bitOp = 0x56 // orps/pd
	}
	g.a.sseRR(prefix, bitOp, xa, xb, false)
	jdone := g.a.JmpPlaceholder()

	g.a.PatchRel32(jdist, g.a.Len())
	if isMax {
		g.a.FMax(xa, xb, f64)
	} else {
		g.a.FMin(xa, xb, f64)
	}
	jdone2 := g.a.JmpPlaceholder()

	g.a.PatchRel32(jnan, g.a.Len())
	g.a.FAdd(xa, xb, f64) // NaN + x = quieted NaN (canonical stays canonical)

	g.a.PatchRel32(jdone, g.a.Len())
	g.a.PatchRel32(jdone2, g.a.Len())
	g.freeFReg(xb)
	g.pushFReg(xa)
}

func (g *cg) fsqrt(f64 bool) {
	a := g.pop()
	src := g.materializeF(a)
	g.a.FSqrt(src, src, f64)
	g.pushFReg(src)
}

// IEEE-754 sign and magnitude bit masks, shared by neg, abs, and copysign.
const (
	fSignMask32 uint32 = 0x80000000
	fMagMask32  uint32 = 0x7FFFFFFF
	fSignMask64 uint64 = 0x8000000000000000
	fMagMask64  uint64 = 0x7FFFFFFFFFFFFFFF
)

func (g *cg) fneg(f64 bool) { g.fsign(0x57, fSignMask64, fSignMask32, f64) } // xorps/pd
func (g *cg) fabs(f64 bool) { g.fsign(0x54, fMagMask64, fMagMask32, f64) }   // andps/pd

func (g *cg) fsign(op byte, mask64 uint64, mask32 uint32, f64 bool) {
	a := g.pop()
	if a.kind == vConst && a.fp { // neg/abs are exact bit ops; fold even for NaN
		mask := uint64(mask32)
		b := uint64(uint32(a.cval))
		if f64 {
			mask, b = mask64, uint64(a.cval)
		}
		if op == 0x57 { // xorps/pd = negate
			b ^= mask
		} else { // andps/pd = abs
			b &= mask
		}
		g.push(ventry{kind: vConst, fp: true, wide: f64, cval: int64(b)})
		return
	}
	x := g.materializeF(a)
	m := g.allocFReg()
	g.loadFMask(m, mask64, mask32, f64)
	var prefix byte
	if f64 {
		prefix = 0x66
	}
	g.a.sseRR(prefix, op, x, m, false)
	g.freeFReg(m)
	g.pushFReg(x)
}

// loadFMask materializes a 32/64-bit bit mask into the XMM register dst (via RSI).
func (g *cg) loadFMask(dst Reg, mask64 uint64, mask32 uint32, f64 bool) {
	if f64 {
		g.a.MovImm64(RSI, mask64)
		g.a.MovGprToXmm(dst, RSI, true)
	} else {
		g.a.MovImm32(RSI, int32(mask32))
		g.a.MovGprToXmm(dst, RSI, false)
	}
}

// Rounding-mode immediates for ROUNDSS/SD; bit 3 (0x08) suppresses the
// precision (inexact) exception, matching wasm's non-trapping semantics.
const (
	roundNearest byte = 0x08 // round to nearest, ties to even
	roundFloor   byte = 0x09 // toward -inf
	roundCeil    byte = 0x0A // toward +inf
	roundTrunc   byte = 0x0B // toward zero
)

func (g *cg) fround(f64 bool, mode byte) {
	a := g.pop()
	x := g.materializeF(a)
	g.a.Round(x, x, f64, mode)
	g.pushFReg(x)
}

// fcopysign computes a with the sign bit of b: (a & ~signbit) | (b & signbit).
func (g *cg) fcopysign(f64 bool) {
	b := g.pop()
	a := g.pop()
	if a.kind == vConst && a.fp && b.kind == vConst && b.fp {
		// copysign is purely bitwise: (a & ~sign) | (b & sign). Folding is
		// bit-exact, including NaN payloads and signed zero.
		mag, sgn := uint64(fMagMask32), uint64(fSignMask32)
		av, bv := uint64(uint32(a.cval)), uint64(uint32(b.cval))
		if f64 {
			mag, sgn, av, bv = fMagMask64, fSignMask64, uint64(a.cval), uint64(b.cval)
		}
		g.push(ventry{kind: vConst, fp: true, wide: f64, cval: int64((av & mag) | (bv & sgn))})
		return
	}
	xa := g.materializeF(a)
	xb := g.materializeF(b)
	var prefix byte
	if f64 {
		prefix = 0x66
	}
	m := g.allocFReg()
	g.loadFMask(m, fMagMask64, fMagMask32, f64)   // magnitude mask
	g.a.sseRR(prefix, 0x54, xa, m, false)         // xa = |a|  (andps/pd)
	g.loadFMask(m, fSignMask64, fSignMask32, f64) // sign mask
	g.a.sseRR(prefix, 0x54, xb, m, false)         // xb = sign(b)
	g.freeFReg(m)
	g.a.sseRR(prefix, 0x56, xa, xb, false) // xa |= xb  (orps/pd)
	g.freeFReg(xb)
	g.pushFReg(xa)
}

// fcmpKind identifies a wasm float comparison (see fcmpKinds).
type fcmpKind uint8

const (
	fcEq fcmpKind = iota
	fcNe
	fcLt
	fcGt
	fcLe
	fcGe
)

// fcmp lowers a wasm float comparison NaN-correctly. ucomis sets ZF=PF=CF=1 on
// an unordered (NaN) compare, so a single setcc is wrong for eq/ne/lt/le. We:
//   - gt/ge: ucomis(a,b) + seta/setae — CF-based, already false when unordered.
//   - lt/le: compare the operands reversed (b,a) and use gt/ge, which is the
//     same NaN-safe CF-based form.
//   - eq: setcc requires ordered AND equal -> sete AND setnp.
//   - ne: unordered OR not-equal -> setne OR setp.
//
// Result: every ordered comparison is false when either operand is NaN; ne is
// true. (WebAssembly semantics.)
func (g *cg) fcmp(kind fcmpKind, f64 bool) {
	b := g.pop()
	a := g.pop()
	if a.kind == vConst && b.kind == vConst && a.fp && b.fp {
		g.push(ventry{kind: vConst, cval: foldFloatCmp(kind, a, b, f64)}) // i32 0/1
		return
	}
	xa := g.materializeF(a)
	xb := g.materializeF(b)
	dst := g.allocReg()
	switch kind {
	case fcGt:
		g.a.Ucomis(xa, xb, f64)
		g.a.SetccReg(CondA, dst)
	case fcGe:
		g.a.Ucomis(xa, xb, f64)
		g.a.SetccReg(CondAE, dst)
	case fcLt: // a<b == b>a; reversed compare keeps the NaN-safe CF form
		g.a.Ucomis(xb, xa, f64)
		g.a.SetccReg(CondA, dst)
	case fcLe: // a<=b == b>=a
		g.a.Ucomis(xb, xa, f64)
		g.a.SetccReg(CondAE, dst)
	case fcEq: // ordered AND equal: ZF=1 and PF=0
		g.a.Ucomis(xa, xb, f64)
		t := g.allocReg()
		g.a.SetccReg(CondE, dst)
		g.a.SetccReg(CondNP, t)
		g.a.AluRR(opAnd.rr, dst, t, false)
		g.freeReg(t)
	case fcNe: // unordered OR not-equal: ZF=0 or PF=1
		g.a.Ucomis(xa, xb, f64)
		t := g.allocReg()
		g.a.SetccReg(CondNE, dst)
		g.a.SetccReg(CondP, t)
		g.a.AluRR(opOr.rr, dst, t, false)
		g.freeReg(t)
	}
	g.freeFReg(xa)
	g.freeFReg(xb)
	g.pushReg(dst)
}

func (g *cg) i2f(f64, srcWide bool) {
	a := g.pop()
	gpr := g.materialize(a)
	xmm := g.allocFReg()
	g.a.Cvtsi2f(xmm, gpr, f64, srcWide)
	g.freeReg(gpr)
	g.pushFReg(xmm)
}

func (g *cg) fpromote() { // f32 -> f64
	a := g.pop()
	x := g.materializeF(a)
	g.a.Cvtss2sd(x, x)
	g.pushFReg(x)
}
func (g *cg) fdemote() { // f64 -> f32
	a := g.pop()
	x := g.materializeF(a)
	g.a.Cvtsd2ss(x, x)
	g.pushFReg(x)
}

func (g *cg) reinterpretIntToFloat(wide bool) {
	a := g.pop()
	if a.kind == vConst && !a.fp { // pure bit copy
		cval := int64(uint32(a.cval)) // i32 -> f32: low 32 bits
		if wide {
			cval = a.cval
		}
		g.push(ventry{kind: vConst, fp: true, wide: wide, cval: cval})
		return
	}
	gpr := g.materialize(a)
	xmm := g.allocFReg()
	g.a.MovGprToXmm(xmm, gpr, wide)
	g.freeReg(gpr)
	g.pushFReg(xmm)
}
func (g *cg) reinterpretFloatToInt(wide bool) {
	a := g.pop()
	if a.kind == vConst && a.fp { // pure bit copy
		cval := int64(int32(uint32(a.cval))) // f32 -> i32: sign-extend low 32
		if wide {
			cval = a.cval
		}
		g.push(ventry{kind: vConst, wide: wide, cval: cval})
		return
	}
	xmm := g.materializeF(a)
	gpr := g.allocReg()
	g.a.MovXmmToGpr(gpr, xmm, wide)
	g.freeFReg(xmm)
	g.pushReg(gpr)
}

func (g *cg) fload(r *wasm.Reader, f64 bool) error {
	if _, err := r.U32(); err != nil { // align
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
	ea, disp := g.memEffectiveAddr(off, size)
	xmm := g.allocFReg()
	g.a.FLoadIdx(xmm, RDI, ea, disp, f64)
	g.freeReg(ea)
	g.pushFReg(xmm)
	return nil
}

func (g *cg) fstore(r *wasm.Reader, f64 bool) error {
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
	val := g.pop()
	xmm := g.materializeF(val)
	ea, disp := g.memEffectiveAddr(off, size)
	g.a.FStoreIdx(RDI, ea, xmm, disp, f64)
	g.freeReg(ea)
	g.freeFReg(xmm)
	return nil
}

func (g *cg) localType(i int) (wasm.ValType, bool) {
	if i < 0 || i >= g.nLocals {
		return wasm.ValType{}, false
	}
	return wasm.LocalType(g.localParams, g.localRuns, uint32(i))
}

func (g *cg) isFloatLocal(i int) bool {
	t, ok := g.localType(i)
	return ok && isFloatType(t)
}

var fcmpKinds = map[byte]fcmpKind{
	0x5B: fcEq, 0x5C: fcNe, 0x5D: fcLt, 0x5E: fcGt, 0x5F: fcLe, 0x60: fcGe, // f32 eq ne lt gt le ge
	0x61: fcEq, 0x62: fcNe, 0x63: fcLt, 0x64: fcGt, 0x65: fcLe, 0x66: fcGe, // f64
}

func isF32Cmp(op byte) bool { return op >= 0x5B && op <= 0x60 }

func isFloatType(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.F32) || wasm.EqualValType(t, wasm.F64)
}
