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

func (g *cg) fbin(op func(dst, src Reg, f64 bool), f64 bool) {
	b := g.pop()
	a := g.pop()
	dst := g.materializeF(a)
	src := g.materializeF(b)
	op(dst, src, f64)
	g.freeFReg(src)
	g.pushFReg(dst)
}

func (g *cg) fsqrt(f64 bool) {
	a := g.pop()
	src := g.materializeF(a)
	g.a.FSqrt(src, src, f64)
	g.pushFReg(src)
}

func (g *cg) fneg(f64 bool) { g.fsign(0x57, 0x8000000000000000, 0x80000000, f64) } // xorps/pd
func (g *cg) fabs(f64 bool) { g.fsign(0x54, 0x7FFFFFFFFFFFFFFF, 0x7FFFFFFF, f64) } // andps/pd

func (g *cg) fsign(op byte, mask64 uint64, mask32 uint32, f64 bool) {
	x := g.materializeF(g.pop())
	m := g.allocFReg()
	var prefix byte
	if f64 {
		prefix = 0x66
		g.a.MovImm64(RSI, mask64)
		g.a.MovGprToXmm(m, RSI, true)
	} else {
		g.a.MovImm32(RSI, int32(mask32))
		g.a.MovGprToXmm(m, RSI, false)
	}
	g.a.sseRR(prefix, op, x, m, false)
	g.freeFReg(m)
	g.pushFReg(x)
}

// NaN handling is conservative: unordered compares are false except ne.
func (g *cg) fcmp(cond Cond, f64 bool) {
	b := g.pop()
	a := g.pop()
	xa := g.materializeF(a)
	xb := g.materializeF(b)
	g.a.Ucomis(xa, xb, f64) // sets CF/ZF/PF (PF=1 if unordered)
	g.freeFReg(xa)
	g.freeFReg(xb)
	dst := g.allocReg()
	g.a.SetccReg(cond, dst)
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

func (g *cg) f2iTrunc(f64, dstWide bool) {
	a := g.pop()
	xmm := g.materializeF(a)
	gpr := g.allocReg()
	g.a.Cvttf2si(gpr, xmm, f64, dstWide)
	g.freeFReg(xmm)
	g.pushReg(gpr)
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
	gpr := g.materialize(a)
	xmm := g.allocFReg()
	g.a.MovGprToXmm(xmm, gpr, wide)
	g.freeReg(gpr)
	g.pushFReg(xmm)
}
func (g *cg) reinterpretFloatToInt(wide bool) {
	a := g.pop()
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
	ea := g.memEffectiveAddr(off, size)
	xmm := g.allocFReg()
	g.a.FLoadIdx(xmm, RDI, ea, f64)
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
	ea := g.memEffectiveAddr(off, size)
	g.a.FStoreIdx(RDI, ea, xmm, f64)
	g.freeReg(ea)
	g.freeFReg(xmm)
	return nil
}

func (g *cg) isFloatLocal(i int) bool {
	return g.localFloat[i]
}

var fcmpCond = map[byte]Cond{
	0x5B: CondE, 0x5C: CondNE, 0x5D: CondB, 0x5E: CondA, 0x5F: CondBE, 0x60: CondAE, // f32 eq ne lt gt le ge
	0x61: CondE, 0x62: CondNE, 0x63: CondB, 0x64: CondA, 0x65: CondBE, 0x66: CondAE, // f64
}

func isF32Cmp(op byte) bool { return op >= 0x5B && op <= 0x60 }

func isF64Type(t wasm.ValType) bool { return t == wasm.F64 }
func isFloatType(t wasm.ValType) bool {
	return t == wasm.F32 || t == wasm.F64
}
