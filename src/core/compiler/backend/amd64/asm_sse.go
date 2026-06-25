package amd64

// SSE/scalar-float encoders. XMM registers reuse Reg indices 0..15.

func sdPrefix(f64 bool) byte {
	if f64 {
		return 0xF2
	}
	return 0xF3
}

func (a *Asm) sseRR(prefix, op byte, reg, rm Reg, w bool) {
	if prefix != 0 {
		a.emit(prefix)
	}
	if w || reg >= 8 || rm >= 8 {
		a.emit(rex(w, reg >= 8, false, rm >= 8))
	}
	a.emit(0x0F, op, 0xC0|((byte(reg)&7)<<3)|byte(rm&7))
}

func (a *Asm) FAdd(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x58, dst, src, false) }
func (a *Asm) FSub(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5C, dst, src, false) }
func (a *Asm) FMul(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x59, dst, src, false) }
func (a *Asm) FDiv(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5E, dst, src, false) }
func (a *Asm) FMin(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5D, dst, src, false) }
func (a *Asm) FMax(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5F, dst, src, false) }
func (a *Asm) FSqrt(dst, src Reg, f64 bool) { a.sseRR(sdPrefix(f64), 0x51, dst, src, false) }

func (a *Asm) FMov(dst, src Reg, f64 bool) { a.sseRR(sdPrefix(f64), 0x10, dst, src, false) }

func (a *Asm) Ucomis(dst, src Reg, f64 bool) {
	var p byte
	if f64 {
		p = 0x66
	}
	a.sseRR(p, 0x2E, dst, src, false)
}

func (a *Asm) Cvtss2sd(dst, src Reg) { a.sseRR(0xF3, 0x5A, dst, src, false) }
func (a *Asm) Cvtsd2ss(dst, src Reg) { a.sseRR(0xF2, 0x5A, dst, src, false) }

func (a *Asm) Cvtsi2f(xmm, gpr Reg, f64, w bool) { a.sseRR(sdPrefix(f64), 0x2A, xmm, gpr, w) }

func (a *Asm) Cvttf2si(gpr, xmm Reg, f64, w bool) { a.sseRR(sdPrefix(f64), 0x2C, gpr, xmm, w) }

func (a *Asm) MovGprToXmm(xmm, gpr Reg, w bool) { a.sseRR(0x66, 0x6E, xmm, gpr, w) }

func (a *Asm) MovXmmToGpr(gpr, xmm Reg, w bool) { a.sseRR(0x66, 0x7E, xmm, gpr, w) }

func (a *Asm) fmemDisp(op byte, xmm, base Reg, disp int32, f64 bool) {
	a.emit(sdPrefix(f64))
	if xmm >= 8 || base >= 8 {
		a.emit(rex(false, xmm >= 8, false, base >= 8))
	}
	a.emit(0x0F, op, 0x80|((byte(xmm)&7)<<3)|byte(base&7)) // mod=10 disp32
	a.imm32(disp)
}

func (a *Asm) FLoadDisp(xmm, base Reg, disp int32, f64 bool) { a.fmemDisp(0x10, xmm, base, disp, f64) }
func (a *Asm) FStoreDisp(base Reg, disp int32, xmm Reg, f64 bool) {
	a.fmemDisp(0x11, xmm, base, disp, f64)
}

func (a *Asm) fmemIdx(op byte, xmm, base, index Reg, f64 bool) {
	a.emit(sdPrefix(f64))
	if xmm >= 8 || index >= 8 || base >= 8 {
		a.emit(rex(false, xmm >= 8, index >= 8, base >= 8))
	}
	a.emit(0x0F, op, ((byte(xmm)&7)<<3)|0x04, ((byte(index)&7)<<3)|byte(base&7))
}

func (a *Asm) FLoadIdx(xmm, base, index Reg, f64 bool)  { a.fmemIdx(0x10, xmm, base, index, f64) }
func (a *Asm) FStoreIdx(base, index, xmm Reg, f64 bool) { a.fmemIdx(0x11, xmm, base, index, f64) }
