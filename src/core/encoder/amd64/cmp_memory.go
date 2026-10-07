package amd64

// CmpImmIdx compares exactly size bytes at [base+index+disp] with imm.
// The 64-bit form sign-extends imm32; narrower forms use its low bits.
func (a *Asm) CmpImmIdx(base, index Reg, disp, imm int32, size int) {
	if size == 2 {
		a.emit(0x66)
		imm = int32(int16(imm))
	}
	if size == 8 || base >= 8 || index >= 8 {
		a.emit(a.rex(size == 8, false, index >= 8, base >= 8))
	}
	op := byte(0x81)
	if size == 1 {
		op = 0x80
	} else if imm >= -128 && imm <= 127 {
		op = 0x83
	}
	a.emit(op)
	a.sibAddr(7, base, index, disp)
	if op != 0x81 {
		a.emit(byte(imm))
	} else if size == 2 {
		a.emit(byte(imm), byte(imm>>8))
	} else {
		a.imm32(imm)
	}
}
