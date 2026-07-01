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

// SseRR exposes the raw two-operand SSE reg,reg encoder for op bytes without a
// dedicated helper (e.g. orps/andps/xorps used by float min/max/neg/copysign).
func (a *Asm) SseRR(prefix, op byte, reg, rm Reg, w bool) { a.sseRR(prefix, op, reg, rm, w) }

func (a *Asm) FAdd(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x58, dst, src, false) }
func (a *Asm) FSub(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5C, dst, src, false) }
func (a *Asm) FMul(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x59, dst, src, false) }
func (a *Asm) FDiv(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5E, dst, src, false) }
func (a *Asm) FMin(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5D, dst, src, false) }
func (a *Asm) FMax(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5F, dst, src, false) }
func (a *Asm) FSqrt(dst, src Reg, f64 bool) { a.sseRR(sdPrefix(f64), 0x51, dst, src, false) }

func (a *Asm) FMov(dst, src Reg, f64 bool) { a.sseRR(sdPrefix(f64), 0x10, dst, src, false) }

// Round emits ROUNDSS/ROUNDSD (SSE4.1): dst = round(src) using rounding-mode
// imm8 (bits 0-1 select nearest/floor/ceil/trunc; bit 3 suppresses precision).
func (a *Asm) Round(dst, src Reg, f64 bool, mode byte) {
	a.emit(0x66)
	if dst >= 8 || src >= 8 {
		a.emit(rex(false, dst >= 8, false, src >= 8))
	}
	op := byte(0x0A) // roundss
	if f64 {
		op = 0x0B // roundsd
	}
	a.emit(0x0F, 0x3A, op, 0xC0|((byte(dst)&7)<<3)|byte(src&7), mode)
}

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

func (a *Asm) fmemIdx(op byte, xmm, base, index Reg, disp int32, f64 bool) {
	a.emit(sdPrefix(f64))
	if xmm >= 8 || index >= 8 || base >= 8 {
		a.emit(rex(false, xmm >= 8, index >= 8, base >= 8))
	}
	a.emit(0x0F, op)
	a.sibAddr(xmm, base, index, disp)
}

func (a *Asm) FLoadIdx(xmm, base, index Reg, disp int32, f64 bool) {
	a.fmemIdx(0x10, xmm, base, index, disp, f64)
}
func (a *Asm) FStoreIdx(base, index, xmm Reg, disp int32, f64 bool) {
	a.fmemIdx(0x11, xmm, base, index, disp, f64)
}
