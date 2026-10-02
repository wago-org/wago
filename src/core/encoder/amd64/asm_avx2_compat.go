package amd64

// Helpers retained from the current encoder while the AVX2 forms live beside
// the existing VEX.128 vocabulary.
func (a *Asm) VFSqrt(dst, s1, s2 Reg, f64 bool) {
	a.vex3RRR(vexPP(f64), 0x51, dst, s1, s2)
}

func (a *Asm) Vcvtdq2ps(dst, src Reg) {
	a.vex3RRReserved(vexMap0F, 0b00, 0x5B, dst, src)
}

func (a *Asm) Vcvtdq2pd(dst, src Reg) {
	a.vex3RRReserved(vexMap0F, 0b10, 0xE6, dst, src)
}

func (a *Asm) Vcvtps2pd(dst, src Reg) {
	a.vex3RRReserved(vexMap0F, 0b00, 0x5A, dst, src)
}

func (a *Asm) Vcvtpd2ps(dst, src Reg) {
	a.vex3RRReserved(vexMap0F, 0b01, 0x5A, dst, src)
}

func (a *Asm) MovdquRipPlaceholder(dst Reg) int {
	a.emit(0xF3)
	if dst >= 8 {
		a.emit(0x44)
	}
	a.emit(0x0F, 0x6F, 0x05|byte(dst&7)<<3)
	off := a.Len()
	a.imm32(0)
	return off
}

func (a *Asm) EmitBytes(bs []byte) { a.B = append(a.B, bs...) }

func (a *Asm) MovsRipPlaceholder(dst Reg, f64 bool) int {
	if f64 {
		a.emit(0xF2)
	} else {
		a.emit(0xF3)
	}
	if dst >= 8 {
		a.emit(0x44)
	}
	a.emit(0x0F, 0x10, 0x05|byte(dst&7)<<3)
	off := a.Len()
	a.imm32(0)
	return off
}

func (a *Asm) VShufps(dst, s1, s2 Reg, imm byte) {
	a.vex3RRIMap(vexMap0F, 0b00, 0xC6, dst, s1, s2, imm)
}

func (a *Asm) VPcmpgtq(dst, s1, s2 Reg) {
	a.vex3RRRMap(vexMap0F38, 0b01, 0x37, dst, s1, s2)
}

func (a *Asm) VPacksswb(dst, s1, s2 Reg) {
	a.vex3RRR(0b01, 0x63, dst, s1, s2)
}

func (a *Asm) VPsllwImm(dst, src Reg, imm byte) {
	a.vexShiftWordImm(6, dst, src, imm)
}

func (a *Asm) VPslldImm(dst, src Reg, imm byte) {
	a.vexShiftDwordImm(6, dst, src, imm)
}

func (a *Asm) VPsllqImm(dst, src Reg, imm byte) {
	a.vexShiftQwordImm(6, dst, src, imm)
}

func (a *Asm) VPmaddubsw(dst, s1, s2 Reg) {
	a.vex3RRRMap(vexMap0F38, 0b01, 0x04, dst, s1, s2)
}

func (a *Asm) VMovmskps(dst, src Reg) {
	a.vex3RRReserved(vexMap0F, 0b00, 0x50, dst, src)
}

func (a *Asm) VMovmskpd(dst, src Reg) {
	a.vex3RRReserved(vexMap0F, 0b01, 0x50, dst, src)
}
