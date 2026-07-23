package amd64

// EVEX helpers used by the wide-SIMD plugin path. The managed surface keeps
// registers below 16, so EVEX.R' and EVEX.V' are always encoded as one.
func (a *Asm) evexPrefix(opcodeMap, pp byte, w bool, dst, src1 Reg, base, index Reg, memory bool) {
	p0 := byte(0xf0) | opcodeMap
	if dst >= 8 {
		p0 &^= 0x10
	}
	if memory {
		if index >= 8 {
			p0 &^= 0x40
		}
		if base >= 8 {
			p0 &^= 0x20
		}
	}
	p1 := byte(0x04) | pp | ((^byte(src1) & 0x0f) << 3)
	if w {
		p1 |= 0x80
	}
	// EVEX.512 (L'L=10), no masking, no broadcast, registers below 16.
	a.emit(0x62, p0, p1, 0x48)
}

func (a *Asm) evexRRR(opcodeMap, pp, op byte, w bool, dst, src1, src2 Reg) {
	a.evexPrefix(opcodeMap, pp, w, dst, src1, 0, 0, false)
	a.emit(op, 0xc0|byte(dst&7)<<3|byte(src2&7))
}

func (a *Asm) evexRR(opcodeMap, pp, op byte, w bool, dst, src Reg) {
	a.evexPrefix(opcodeMap, pp, w, dst, 0, 0, 0, false)
	a.emit(op, 0xc0|byte(dst&7)<<3|byte(src&7))
}

func (a *Asm) evexMemIdx(opcodeMap, pp, op byte, w bool, reg, base, index Reg, disp int32) {
	a.evexPrefix(opcodeMap, pp, w, reg, 0, base, index, true)
	a.emit(op, 0x80|byte(reg&7)<<3|0x04)
	a.emit(byte(index&7)<<3 | byte(base&7))
	a.imm32(disp)
}

// ZMovdqu64 uses an unaligned 64-byte memory operand. W=1 selects the
// AVX-512F vmovdqu64 form and avoids imposing alignment on guest pointers.
func (a *Asm) ZMovdqu64LoadIdx(dst, base, index Reg, disp int32) {
	a.evexMemIdx(vexMap0F, 0b10, 0x6f, true, dst, base, index, disp)
}

func (a *Asm) ZMovdqu64StoreIdx(base, index, src Reg, disp int32) {
	a.evexMemIdx(vexMap0F, 0b10, 0x7f, true, src, base, index, disp)
}

// ZSIMDRRR emits one unmasked 512-bit EVEX register operation. It is kept
// generic so Wasm opcode selection remains in the compiler backend.
func (a *Asm) ZSIMDRRR(opcodeMap, pp, op byte, w bool, dst, src1, src2 Reg) {
	a.evexRRR(opcodeMap, pp, op, w, dst, src1, src2)
}

func (a *Asm) ZSIMDRR(opcodeMap, pp, op byte, w bool, dst, src Reg) {
	a.evexRR(opcodeMap, pp, op, w, dst, src)
}

func (a *Asm) ZPternlogd(dst, src1, src2 Reg, imm byte) {
	a.evexRRR(vexMap0F3A, 1, 0x25, false, dst, src1, src2)
	a.emit(imm)
}
