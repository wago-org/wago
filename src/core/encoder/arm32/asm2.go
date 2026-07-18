package arm32

// Adds/Subs are flag-setting forms used for multiword arithmetic.
func (a *Asm) Adds(rd, rn, rm Reg) bool { return a.dpReg(0xeb10, rd, rn, rm, 0, 0) }
func (a *Asm) Subs(rd, rn, rm Reg) bool { return a.dpReg(0xebb0, rd, rn, rm, 0, 0) }

// Add64 emits little-endian two-word addition. It is safe for destinations to
// alias either corresponding input because carry is held in APSR between words.
func (a *Asm) Add64(dstLo, dstHi, aLo, aHi, bLo, bHi Reg) bool {
	return a.Adds(dstLo, aLo, bLo) && a.Adc(dstHi, aHi, bHi)
}

// Sub64 emits little-endian two-word subtraction using APSR carry-not-borrow.
func (a *Asm) Sub64(dstLo, dstHi, aLo, aHi, bLo, bHi Reg) bool {
	return a.Subs(dstLo, aLo, bLo) && a.Sbc(dstHi, aHi, bHi)
}

// Mul64 emits the low 64 bits of a two-word product. dstLo/dstHi and scratch
// must be distinct from all input registers.
func (a *Asm) Mul64(dstLo, dstHi, aLo, aHi, bLo, bHi, scratch Reg) bool {
	if !a.Umull(dstLo, dstHi, aLo, bLo) {
		return false
	}
	if !a.Mul(scratch, aLo, bHi) || !a.Add(dstHi, dstHi, scratch) {
		return false
	}
	return a.Mul(scratch, aHi, bLo) && a.Add(dstHi, dstHi, scratch)
}

// Quad is the little-endian four-GPR representation used by a 32-bit SWAR
// backend for WebAssembly v128 values.
type Quad [4]Reg

func validQuad(q Quad) bool {
	seen := uint16(0)
	for _, r := range q {
		if !validCoreReg(r) || seen&(1<<r) != 0 {
			return false
		}
		seen |= 1 << r
	}
	return true
}

func (a *Asm) quad3(dst, left, right Quad, op func(Reg, Reg, Reg) bool) bool {
	if !validQuad(dst) || !validQuad(left) || !validQuad(right) {
		return false
	}
	for i := range dst {
		if !op(dst[i], left[i], right[i]) {
			return false
		}
	}
	return true
}

func (a *Asm) And128(dst, left, right Quad) bool   { return a.quad3(dst, left, right, a.And) }
func (a *Asm) Orr128(dst, left, right Quad) bool   { return a.quad3(dst, left, right, a.Orr) }
func (a *Asm) Eor128(dst, left, right Quad) bool   { return a.quad3(dst, left, right, a.Eor) }
func (a *Asm) AddI32x4(dst, left, right Quad) bool { return a.quad3(dst, left, right, a.Add) }
func (a *Asm) SubI32x4(dst, left, right Quad) bool { return a.quad3(dst, left, right, a.Sub) }

// F64Abs/F64Neg/F64Copysign lower the bitwise WebAssembly f64 operations over
// little-endian register pairs. They never touch the scalar FP unit.
func (a *Asm) F64Abs(lo, hi, mask Reg) bool {
	return a.MovImm32(mask, 0x7fffffff) && a.And(hi, hi, mask)
}
func (a *Asm) F64Neg(lo, hi, mask Reg) bool {
	return a.MovImm32(mask, 0x80000000) && a.Eor(hi, hi, mask)
}
func (a *Asm) F64Copysign(dstLo, dstHi, magLo, magHi, signHi, magMask, signMask Reg) bool {
	return a.MovReg(dstLo, magLo) &&
		a.MovImm32(magMask, 0x7fffffff) && a.And(dstHi, magHi, magMask) &&
		a.MovImm32(signMask, 0x80000000) && a.And(signMask, signHi, signMask) &&
		a.Orr(dstHi, dstHi, signMask)
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
// destroy right. The three scratch registers must not overlap either quad.
func (a *Asm) PackedAddSub(left, right Quad, width uint8, sub bool, lowReg, highReg, tmp Reg) bool {
	low, high, ok := packedMasks(width)
	if !ok || !validQuad(left) || !validQuad(right) {
		return false
	}
	if !a.MovImm32(lowReg, low) || !a.MovImm32(highReg, high) {
		return false
	}
	for i := range left {
		x, y := left[i], right[i]
		if !sub {
			if !a.And(tmp, x, lowReg) || !a.Eor(x, x, y) || !a.And(x, x, highReg) ||
				!a.And(y, y, lowReg) || !a.Add(tmp, tmp, y) || !a.Eor(x, x, tmp) {
				return false
			}
			continue
		}
		if !a.Eor(tmp, x, y) || !a.Mvn(tmp, tmp) || !a.And(tmp, tmp, highReg) ||
			!a.Orr(x, x, highReg) || !a.And(y, y, lowReg) || !a.Sub(x, x, y) || !a.Eor(x, x, tmp) {
			return false
		}
	}
	return true
}
