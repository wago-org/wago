// Package arm32 writes Thumb-2 machine code for Armv8-M Mainline cores such as
// the Cortex-M33 in RP2350.
//
// The baseline deliberately uses integer Thumb-2 instructions only. It does not
// require MVE/Helium, NEON, or a scalar floating-point register file; f64 and
// WebAssembly v128 values are lowered through integer pairs/quads and SWAR or
// helper calls by the compiler backend. Most operations use fixed 32-bit Thumb
// encodings to keep patching and code publication predictable. Architectural
// 16-bit control/system instructions are emitted where Thumb has no 32-bit
// benefit.
package arm32

// Reg names an Arm core register.
type Reg uint8

const (
	R0 Reg = iota
	R1
	R2
	R3
	R4
	R5
	R6
	R7
	R8
	R9
	R10
	R11
	R12
	SP
	LR
	PC
)

// Cond is an Arm condition code.
type Cond uint8

const (
	CondEQ Cond = iota
	CondNE
	CondCS
	CondCC
	CondMI
	CondPL
	CondVS
	CondVC
	CondHI
	CondLS
	CondGE
	CondLT
	CondGT
	CondLE
	CondAL
)

func (c Cond) Invert() Cond {
	if c >= CondAL {
		panic("arm32: AL has no conditional inverse")
	}
	return c ^ 1
}

// Asm accumulates little-endian Thumb instructions. A 32-bit Thumb instruction
// is stored as its first 16-bit halfword followed by its second halfword.
type Asm struct{ B []byte }

func (a *Asm) half(h uint16) { a.B = append(a.B, byte(h), byte(h>>8)) }
func (a *Asm) wide(first, second uint16) {
	a.half(first)
	a.half(second)
}
func (a *Asm) Len() int { return len(a.B) }
func (a *Asm) Grow(n int) {
	if n <= 0 || cap(a.B)-len(a.B) >= n {
		return
	}
	b := make([]byte, len(a.B), len(a.B)+n)
	copy(b, a.B)
	a.B = b
}
func (a *Asm) halfAt(at int) uint16 { return uint16(a.B[at]) | uint16(a.B[at+1])<<8 }
func (a *Asm) patchHalf(at int, h uint16) {
	a.B[at], a.B[at+1] = byte(h), byte(h>>8)
}
func validReg(r Reg) bool     { return r <= PC }
func validCoreReg(r Reg) bool { return r < PC }

// MOVW/MOVT immediate layout is imm4:i:imm3:imm8.
func (a *Asm) movHalf(rd Reg, imm uint16, top bool) bool {
	if !validCoreReg(rd) {
		return false
	}
	first := uint16(0xf240)
	if top {
		first = 0xf2c0
	}
	first |= uint16(imm>>12)&0xf | (uint16(imm>>11)&1)<<10
	second := uint16(rd)<<8 | (uint16(imm>>8)&7)<<12 | uint16(imm&0xff)
	a.wide(first, second)
	return true
}
func (a *Asm) Movw(rd Reg, imm uint16) bool { return a.movHalf(rd, imm, false) }
func (a *Asm) Movt(rd Reg, imm uint16) bool { return a.movHalf(rd, imm, true) }
func (a *Asm) MovImm32(rd Reg, value uint32) bool {
	if !a.Movw(rd, uint16(value)) {
		return false
	}
	if value>>16 != 0 {
		_ = a.Movt(rd, uint16(value>>16))
	}
	return true
}

// dpReg emits a Thumb-2 shifted-register data-processing instruction.
func (a *Asm) dpReg(base uint16, rd, rn, rm Reg, shiftType uint8, shift uint8) bool {
	if !validCoreReg(rd) || !validReg(rn) || !validCoreReg(rm) || shiftType > 3 || shift > 31 {
		return false
	}
	first := base | uint16(rn)
	second := uint16(shift>>2)<<12 | uint16(rd)<<8 | uint16(shift&3)<<6 | uint16(shiftType)<<4 | uint16(rm)
	a.wide(first, second)
	return true
}

func (a *Asm) MovReg(rd, rm Reg) bool              { return a.dpReg(0xea40, rd, PC, rm, 0, 0) }
func (a *Asm) Mvn(rd, rm Reg) bool                 { return a.dpReg(0xea60, rd, PC, rm, 0, 0) }
func (a *Asm) Add(rd, rn, rm Reg) bool             { return a.dpReg(0xeb00, rd, rn, rm, 0, 0) }
func (a *Asm) Sub(rd, rn, rm Reg) bool             { return a.dpReg(0xeba0, rd, rn, rm, 0, 0) }
func (a *Asm) Adc(rd, rn, rm Reg) bool             { return a.dpReg(0xeb40, rd, rn, rm, 0, 0) }
func (a *Asm) Sbc(rd, rn, rm Reg) bool             { return a.dpReg(0xeb60, rd, rn, rm, 0, 0) }
func (a *Asm) And(rd, rn, rm Reg) bool             { return a.dpReg(0xea00, rd, rn, rm, 0, 0) }
func (a *Asm) Bic(rd, rn, rm Reg) bool             { return a.dpReg(0xea20, rd, rn, rm, 0, 0) }
func (a *Asm) Orr(rd, rn, rm Reg) bool             { return a.dpReg(0xea40, rd, rn, rm, 0, 0) }
func (a *Asm) Orn(rd, rn, rm Reg) bool             { return a.dpReg(0xea60, rd, rn, rm, 0, 0) }
func (a *Asm) Eor(rd, rn, rm Reg) bool             { return a.dpReg(0xea80, rd, rn, rm, 0, 0) }
func (a *Asm) LslImm(rd, rm Reg, shift uint8) bool { return a.dpReg(0xea40, rd, PC, rm, 0, shift) }
func (a *Asm) LsrImm(rd, rm Reg, shift uint8) bool {
	if shift == 0 || shift > 32 {
		return false
	}
	if shift == 32 {
		shift = 0
	}
	return a.dpReg(0xea40, rd, PC, rm, 1, shift)
}
func (a *Asm) AsrImm(rd, rm Reg, shift uint8) bool {
	if shift == 0 || shift > 32 {
		return false
	}
	if shift == 32 {
		shift = 0
	}
	return a.dpReg(0xea40, rd, PC, rm, 2, shift)
}
func (a *Asm) RorImm(rd, rm Reg, shift uint8) bool {
	if shift == 0 || shift > 31 {
		return false
	}
	return a.dpReg(0xea40, rd, PC, rm, 3, shift)
}

func (a *Asm) shiftReg(base uint16, rd, rn, rm Reg) bool {
	if !validCoreReg(rd) || !validCoreReg(rn) || !validCoreReg(rm) {
		return false
	}
	a.wide(base|uint16(rn), 0xf000|uint16(rd)<<8|uint16(rm))
	return true
}
func (a *Asm) Lsl(rd, rn, rm Reg) bool { return a.shiftReg(0xfa00, rd, rn, rm) }
func (a *Asm) Lsr(rd, rn, rm Reg) bool { return a.shiftReg(0xfa20, rd, rn, rm) }
func (a *Asm) Asr(rd, rn, rm Reg) bool { return a.shiftReg(0xfa40, rd, rn, rm) }
func (a *Asm) Ror(rd, rn, rm Reg) bool { return a.shiftReg(0xfa60, rd, rn, rm) }

func (a *Asm) Mul(rd, rn, rm Reg) bool {
	if !validCoreReg(rd) || !validCoreReg(rn) || !validCoreReg(rm) {
		return false
	}
	a.wide(0xfb00|uint16(rn), 0xf000|uint16(rd)<<8|uint16(rm))
	return true
}
func (a *Asm) mull(base uint16, rdLo, rdHi, rn, rm Reg) bool {
	if !validCoreReg(rdLo) || !validCoreReg(rdHi) || !validCoreReg(rn) || !validCoreReg(rm) || rdLo == rdHi {
		return false
	}
	a.wide(base|uint16(rn), uint16(rdLo)<<12|uint16(rdHi)<<8|uint16(rm))
	return true
}
func (a *Asm) Umull(rdLo, rdHi, rn, rm Reg) bool { return a.mull(0xfba0, rdLo, rdHi, rn, rm) }
func (a *Asm) Smull(rdLo, rdHi, rn, rm Reg) bool { return a.mull(0xfb80, rdLo, rdHi, rn, rm) }

func (a *Asm) div(base uint16, rd, rn, rm Reg) bool {
	if !validCoreReg(rd) || !validCoreReg(rn) || !validCoreReg(rm) {
		return false
	}
	a.wide(base|uint16(rn), 0xf0f0|uint16(rd)<<8|uint16(rm))
	return true
}
func (a *Asm) Udiv(rd, rn, rm Reg) bool { return a.div(0xfbb0, rd, rn, rm) }
func (a *Asm) Sdiv(rd, rn, rm Reg) bool { return a.div(0xfb90, rd, rn, rm) }

// Cmp emits CMP.W rn,rm and updates APSR flags.
func (a *Asm) Cmp(rn, rm Reg) bool {
	if !validCoreReg(rn) || !validCoreReg(rm) {
		return false
	}
	a.wide(0xebb0|uint16(rn), 0x0f00|uint16(rm))
	return true
}

func (a *Asm) loadStore(base uint16, rt, rn Reg, off uint16) bool {
	if !validCoreReg(rt) || !validCoreReg(rn) || off > 4095 {
		return false
	}
	a.wide(base|uint16(rn), uint16(rt)<<12|off)
	return true
}
func (a *Asm) Ldr(rt, rn Reg, off uint16) bool  { return a.loadStore(0xf8d0, rt, rn, off) }
func (a *Asm) Str(rt, rn Reg, off uint16) bool  { return a.loadStore(0xf8c0, rt, rn, off) }
func (a *Asm) Ldrb(rt, rn Reg, off uint16) bool { return a.loadStore(0xf890, rt, rn, off) }
func (a *Asm) Strb(rt, rn Reg, off uint16) bool { return a.loadStore(0xf880, rt, rn, off) }
func (a *Asm) Ldrh(rt, rn Reg, off uint16) bool { return a.loadStore(0xf8b0, rt, rn, off) }
func (a *Asm) Strh(rt, rn Reg, off uint16) bool { return a.loadStore(0xf8a0, rt, rn, off) }
func (a *Asm) Ldrsb(rt, rn Reg, off uint8) bool {
	return a.loadStore(0xf990, rt, rn, uint16(off))
}
func (a *Asm) Ldrsh(rt, rn Reg, off uint8) bool {
	return a.loadStore(0xf9b0, rt, rn, uint16(off))
}

// Branch emits a fixed-size unconditional B.W placeholder. PatchBranch computes
// a PC-relative Thumb displacement once layout is known.
func (a *Asm) Branch() int {
	at := a.Len()
	a.wide(0xf000, 0xb800)
	return at
}
func (a *Asm) Call() int {
	at := a.Len()
	a.wide(0xf000, 0xf800)
	return at
}
func encodeBranchOffset(at, target int) (s, i1, i2, imm10, imm11 uint32, ok bool) {
	d := int64(target - (at + 4))
	if d&1 != 0 || d < -(1<<24) || d > (1<<24)-2 {
		return 0, 0, 0, 0, 0, false
	}
	u := uint32(d) & 0x01ffffff
	return u >> 24 & 1, u >> 23 & 1, u >> 22 & 1, u >> 12 & 0x3ff, u >> 1 & 0x7ff, true
}
func (a *Asm) patchBranch(at, target int, link bool) bool {
	s, i1, i2, imm10, imm11, ok := encodeBranchOffset(at, target)
	if !ok {
		return false
	}
	j1, j2 := (^uint32(0)^i1^s)&1, (^uint32(0)^i2^s)&1
	first := uint16(0xf000 | s<<10 | imm10)
	secondBase := uint32(0x9000)
	if link {
		secondBase = 0xd000
	}
	second := uint16(secondBase | j1<<13 | j2<<11 | imm11)
	a.patchHalf(at, first)
	a.patchHalf(at+2, second)
	return true
}
func (a *Asm) PatchBranch(at, target int) bool { return a.patchBranch(at, target, false) }
func (a *Asm) PatchCall(at, target int) bool   { return a.patchBranch(at, target, true) }

// FarBcond is a fixed six-byte inverse short branch around B.W.
func (a *Asm) FarBcond(c Cond) int {
	if c >= CondAL {
		panic("arm32: invalid conditional branch")
	}
	at := a.Len()
	a.half(0xd000 | uint16(c.Invert())<<8 | 1) // PC=at+4, skip target=at+6.
	a.Branch()
	return at
}
func (a *Asm) PatchFarBranch(at, target int) bool { return a.PatchBranch(at+2, target) }

func (a *Asm) Bx(rm Reg) bool {
	if !validCoreReg(rm) {
		return false
	}
	a.half(0x4700 | uint16(rm)<<3)
	return true
}
func (a *Asm) Blx(rm Reg) bool {
	if !validCoreReg(rm) {
		return false
	}
	a.half(0x4780 | uint16(rm)<<3)
	return true
}
func (a *Asm) Ret() { _ = a.Bx(LR) }

func (a *Asm) Nop()           { a.half(0xbf00) }
func (a *Asm) Svc(imm uint8)  { a.half(0xdf00 | uint16(imm)) }
func (a *Asm) Bkpt(imm uint8) { a.half(0xbe00 | uint16(imm)) }
func (a *Asm) Dmb()           { a.wide(0xf3bf, 0x8f5f) }
func (a *Asm) Dsb()           { a.wide(0xf3bf, 0x8f4f) }
func (a *Asm) Isb()           { a.wide(0xf3bf, 0x8f6f) }
func (a *Asm) Align4() {
	for len(a.B)%4 != 0 {
		a.Nop()
	}
}
func (a *Asm) Align16() {
	for len(a.B)%16 != 0 {
		a.Nop()
	}
}
