package riscv32

// MemoryOrder maps to the RISC-V aq/rl bits: bit 1 is acquire and bit 0 is
// release.
type MemoryOrder uint8

const (
	OrderRelaxed MemoryOrder = iota
	OrderRelease
	OrderAcquire
	OrderAcquireRelease
)

func (a *Asm) atomic(funct5 uint32, rd, base, src Reg, order MemoryOrder) {
	a.word(0x2f | r(rd)<<7 | 2<<12 | r(base)<<15 | r(src)<<20 |
		(uint32(order)&3)<<25 | (funct5&0x1f)<<27)
}

func (a *Asm) Lr32(rd, base Reg, order MemoryOrder) { a.atomic(0x02, rd, base, Zero, order) }
func (a *Asm) Sc32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x03, rd, base, src, order)
}
func (a *Asm) AmoSwap32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x01, rd, base, src, order)
}
func (a *Asm) AmoAdd32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x00, rd, base, src, order)
}
func (a *Asm) AmoXor32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x04, rd, base, src, order)
}
func (a *Asm) AmoAnd32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x0c, rd, base, src, order)
}
func (a *Asm) AmoOr32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x08, rd, base, src, order)
}
func (a *Asm) AmoMin32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x10, rd, base, src, order)
}
func (a *Asm) AmoMax32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x14, rd, base, src, order)
}
func (a *Asm) AmoMinu32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x18, rd, base, src, order)
}
func (a *Asm) AmoMaxu32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x1c, rd, base, src, order)
}

func (a *Asm) csr(funct3 uint32, rd Reg, csr uint16, rs1 Reg) {
	a.word(0x73 | r(rd)<<7 | (funct3&7)<<12 | r(rs1)<<15 | (uint32(csr)&0xfff)<<20)
}
func (a *Asm) csri(funct3 uint32, rd Reg, csr uint16, zimm uint8) bool {
	if zimm > 31 {
		return false
	}
	a.word(0x73 | r(rd)<<7 | (funct3&7)<<12 | uint32(zimm)<<15 | (uint32(csr)&0xfff)<<20)
	return true
}
func (a *Asm) Csrrw(rd Reg, csr uint16, rs Reg)           { a.csr(1, rd, csr, rs) }
func (a *Asm) Csrrs(rd Reg, csr uint16, rs Reg)           { a.csr(2, rd, csr, rs) }
func (a *Asm) Csrrc(rd Reg, csr uint16, rs Reg)           { a.csr(3, rd, csr, rs) }
func (a *Asm) Csrrwi(rd Reg, csr uint16, zimm uint8) bool { return a.csri(5, rd, csr, zimm) }
func (a *Asm) Csrrsi(rd Reg, csr uint16, zimm uint8) bool { return a.csri(6, rd, csr, zimm) }
func (a *Asm) Csrrci(rd Reg, csr uint16, zimm uint8) bool { return a.csri(7, rd, csr, zimm) }

// Add64 emits little-endian two-word addition. Destination registers must not
// alias input registers; scratch receives the carry from the low word.
func (a *Asm) Add64(dstLo, dstHi, aLo, aHi, bLo, bHi, scratch Reg) {
	a.Add(dstLo, aLo, bLo)
	a.Sltu(scratch, dstLo, aLo)
	a.Add(dstHi, aHi, bHi)
	a.Add(dstHi, dstHi, scratch)
}

// Sub64 emits little-endian two-word subtraction. Destination registers may
// alias inputs because borrow is captured before either input is overwritten.
func (a *Asm) Sub64(dstLo, dstHi, aLo, aHi, bLo, bHi, scratch Reg) {
	a.Sltu(scratch, aLo, bLo)
	a.Sub(dstLo, aLo, bLo)
	a.Sub(dstHi, aHi, bHi)
	a.Sub(dstHi, dstHi, scratch)
}

// Mul64 emits the low 64 bits of a two-word product. Destinations and scratch
// registers must be distinct from inputs. This is the modular multiplication
// required by WebAssembly i64.mul.
func (a *Asm) Mul64(dstLo, dstHi, aLo, aHi, bLo, bHi, scratch1, scratch2 Reg) {
	a.Mul(dstLo, aLo, bLo)
	a.Mulhu(dstHi, aLo, bLo)
	a.Mul(scratch1, aLo, bHi)
	a.Add(dstHi, dstHi, scratch1)
	a.Mul(scratch2, aHi, bLo)
	a.Add(dstHi, dstHi, scratch2)
}

// Quad is the little-endian four-GPR representation used by a 32-bit SWAR
// backend for WebAssembly v128 values.
type Quad [4]Reg

func validQuad(q Quad) bool {
	seen := uint32(0)
	for _, r := range q {
		if r == Zero || r > X31 || seen&(1<<r) != 0 {
			return false
		}
		seen |= 1 << r
	}
	return true
}

func (a *Asm) quad3(dst, left, right Quad, op func(Reg, Reg, Reg)) bool {
	if !validQuad(dst) || !validQuad(left) || !validQuad(right) {
		return false
	}
	for i := range dst {
		op(dst[i], left[i], right[i])
	}
	return true
}

func (a *Asm) And128(dst, left, right Quad) bool   { return a.quad3(dst, left, right, a.And) }
func (a *Asm) Orr128(dst, left, right Quad) bool   { return a.quad3(dst, left, right, a.Or) }
func (a *Asm) Eor128(dst, left, right Quad) bool   { return a.quad3(dst, left, right, a.Xor) }
func (a *Asm) AddI32x4(dst, left, right Quad) bool { return a.quad3(dst, left, right, a.Add) }
func (a *Asm) SubI32x4(dst, left, right Quad) bool { return a.quad3(dst, left, right, a.Sub) }

// F64Abs/F64Neg/F64Copysign lower bitwise WebAssembly f64 operations over
// little-endian register pairs without requiring F or D extensions.
func (a *Asm) F64Abs(lo, hi, mask Reg) {
	a.MovImm32(mask, 0x7fffffff)
	a.And(hi, hi, mask)
}
func (a *Asm) F64Neg(lo, hi, mask Reg) {
	a.MovImm32(mask, 0x80000000)
	a.Xor(hi, hi, mask)
}
func (a *Asm) F64Copysign(dstLo, dstHi, magLo, magHi, signHi, magMask, signMask Reg) {
	a.MovReg(dstLo, magLo)
	a.MovImm32(magMask, 0x7fffffff)
	a.And(dstHi, magHi, magMask)
	a.MovImm32(signMask, 0x80000000)
	a.And(signMask, signHi, signMask)
	a.Or(dstHi, dstHi, signMask)
}

func packedMasks(width uint8) (low, high uint32, ok bool) {
	switch width {
	case 8:
		return 0x7f7f7f7f, 0x80808080, true
	case 16:
		return 0x7fff7fff, 0x80008000, true
	default:
		return 0, 0, false
	}
}

// PackedAddSub mutates left into lane-wise modular i8x16/i16x8 add/sub and may
// destroy right. The scratch registers must not overlap either quad.
func (a *Asm) PackedAddSub(left, right Quad, width uint8, sub bool, lowReg, highReg, tmp Reg) bool {
	low, high, ok := packedMasks(width)
	if !ok || !validQuad(left) || !validQuad(right) {
		return false
	}
	a.MovImm32(lowReg, low)
	a.MovImm32(highReg, high)
	for i := range left {
		x, y := left[i], right[i]
		if !sub {
			a.And(tmp, x, lowReg)
			a.Xor(x, x, y)
			a.And(x, x, highReg)
			a.And(y, y, lowReg)
			a.Add(tmp, tmp, y)
			a.Xor(x, x, tmp)
			continue
		}
		a.Xor(tmp, x, y)
		a.Not(tmp, tmp)
		a.And(tmp, tmp, highReg)
		a.Or(x, x, highReg)
		a.And(y, y, lowReg)
		a.Sub(x, x, y)
		a.Xor(x, x, tmp)
	}
	return true
}
