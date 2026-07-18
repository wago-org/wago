// Package riscv32 writes fixed-width RV32 machine instructions.
//
// It is intentionally architecture-only: WebAssembly value representation,
// helper ABIs, traps, and the embedded runtime belong in the compiler/runtime
// packages. The baseline is RV32IM with optional A/Zicsr methods in asm2.go;
// compressed, floating-point, vector, and Hazard3-specific instructions are not
// required. This keeps generated code portable to small RV32 cores and leaves
// f32/f64 and v128 semantics to integer/SWAR lowering.
package riscv32

// Reg is a physical integer register number.
type Reg uint8

const (
	X0 Reg = iota
	X1
	X2
	X3
	X4
	X5
	X6
	X7
	X8
	X9
	X10
	X11
	X12
	X13
	X14
	X15
	X16
	X17
	X18
	X19
	X20
	X21
	X22
	X23
	X24
	X25
	X26
	X27
	X28
	X29
	X30
	X31
)

const (
	Zero = X0
	RA   = X1
	SP   = X2
	GP   = X3
	TP   = X4
	T0   = X5
	T1   = X6
	T2   = X7
	S0   = X8
	FP   = X8
	S1   = X9
	A0   = X10
	A1   = X11
	A2   = X12
	A3   = X13
	A4   = X14
	A5   = X15
	A6   = X16
	A7   = X17
	S2   = X18
	S3   = X19
	S4   = X20
	S5   = X21
	S6   = X22
	S7   = X23
	S8   = X24
	S9   = X25
	S10  = X26
	S11  = X27
	T3   = X28
	T4   = X29
	T5   = X30
	T6   = X31
)

// Cond is a base integer branch relation.
type Cond uint8

const (
	CondEQ  Cond = 0
	CondNE  Cond = 1
	CondLT  Cond = 4
	CondGE  Cond = 5
	CondLTU Cond = 6
	CondGEU Cond = 7
)

func (c Cond) Invert() Cond { return c ^ 1 }

// Asm accumulates little-endian 32-bit instruction words. Its zero value is
// ready for use.
type Asm struct{ B []byte }

func (a *Asm) word(w uint32) {
	a.B = append(a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
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
func (a *Asm) wordAt(pos int) uint32 {
	return uint32(a.B[pos]) | uint32(a.B[pos+1])<<8 | uint32(a.B[pos+2])<<16 | uint32(a.B[pos+3])<<24
}
func (a *Asm) replaceWordFields(pos int, mask, fields uint32) {
	w := a.wordAt(pos)&^mask | fields&mask
	a.B[pos], a.B[pos+1], a.B[pos+2], a.B[pos+3] = byte(w), byte(w>>8), byte(w>>16), byte(w>>24)
}
func (a *Asm) PatchU32(at int, val uint32) {
	a.B[at], a.B[at+1], a.B[at+2], a.B[at+3] = byte(val), byte(val>>8), byte(val>>16), byte(val>>24)
}

func r(x Reg) uint32 { return uint32(x) & 31 }
func fitsSigned(v int64, bits uint) bool {
	min, max := -(int64(1) << (bits - 1)), (int64(1)<<(bits-1))-1
	return v >= min && v <= max
}
func (a *Asm) rtype(opcode, funct3, funct7 uint32, rd, rs1, rs2 Reg) {
	a.word(opcode | r(rd)<<7 | (funct3&7)<<12 | r(rs1)<<15 | r(rs2)<<20 | (funct7&0x7f)<<25)
}
func (a *Asm) itype(opcode, funct3 uint32, rd, rs1 Reg, imm int32) bool {
	if !fitsSigned(int64(imm), 12) {
		return false
	}
	a.word(opcode | r(rd)<<7 | (funct3&7)<<12 | r(rs1)<<15 | (uint32(imm)&0xfff)<<20)
	return true
}
func (a *Asm) stype(opcode, funct3 uint32, rs1, rs2 Reg, imm int32) bool {
	if !fitsSigned(int64(imm), 12) {
		return false
	}
	u := uint32(imm) & 0xfff
	a.word(opcode | (u&0x1f)<<7 | (funct3&7)<<12 | r(rs1)<<15 | r(rs2)<<20 | (u>>5)<<25)
	return true
}
func (a *Asm) utype(opcode uint32, rd Reg, imm20 int32) bool {
	if !fitsSigned(int64(imm20), 20) {
		return false
	}
	a.word(opcode | r(rd)<<7 | (uint32(imm20)&0xfffff)<<12)
	return true
}

// RV32I register operations.
func (a *Asm) Add(rd, rs1, rs2 Reg)  { a.rtype(0x33, 0, 0x00, rd, rs1, rs2) }
func (a *Asm) Sub(rd, rs1, rs2 Reg)  { a.rtype(0x33, 0, 0x20, rd, rs1, rs2) }
func (a *Asm) Sll(rd, rs1, rs2 Reg)  { a.rtype(0x33, 1, 0x00, rd, rs1, rs2) }
func (a *Asm) Slt(rd, rs1, rs2 Reg)  { a.rtype(0x33, 2, 0x00, rd, rs1, rs2) }
func (a *Asm) Sltu(rd, rs1, rs2 Reg) { a.rtype(0x33, 3, 0x00, rd, rs1, rs2) }
func (a *Asm) Xor(rd, rs1, rs2 Reg)  { a.rtype(0x33, 4, 0x00, rd, rs1, rs2) }
func (a *Asm) Srl(rd, rs1, rs2 Reg)  { a.rtype(0x33, 5, 0x00, rd, rs1, rs2) }
func (a *Asm) Sra(rd, rs1, rs2 Reg)  { a.rtype(0x33, 5, 0x20, rd, rs1, rs2) }
func (a *Asm) Or(rd, rs1, rs2 Reg)   { a.rtype(0x33, 6, 0x00, rd, rs1, rs2) }
func (a *Asm) And(rd, rs1, rs2 Reg)  { a.rtype(0x33, 7, 0x00, rd, rs1, rs2) }

func (a *Asm) Addi(rd, rs1 Reg, imm int32) bool  { return a.itype(0x13, 0, rd, rs1, imm) }
func (a *Asm) Slti(rd, rs1 Reg, imm int32) bool  { return a.itype(0x13, 2, rd, rs1, imm) }
func (a *Asm) Sltiu(rd, rs1 Reg, imm int32) bool { return a.itype(0x13, 3, rd, rs1, imm) }
func (a *Asm) Xori(rd, rs1 Reg, imm int32) bool  { return a.itype(0x13, 4, rd, rs1, imm) }
func (a *Asm) Ori(rd, rs1 Reg, imm int32) bool   { return a.itype(0x13, 6, rd, rs1, imm) }
func (a *Asm) Andi(rd, rs1 Reg, imm int32) bool  { return a.itype(0x13, 7, rd, rs1, imm) }

func (a *Asm) shiftImm(funct3, funct7 uint32, rd, rs1 Reg, sh uint8) bool {
	if sh >= 32 {
		return false
	}
	a.word(0x13 | r(rd)<<7 | funct3<<12 | r(rs1)<<15 | uint32(sh)<<20 | funct7<<25)
	return true
}
func (a *Asm) Slli(rd, rs1 Reg, sh uint8) bool { return a.shiftImm(1, 0x00, rd, rs1, sh) }
func (a *Asm) Srli(rd, rs1 Reg, sh uint8) bool { return a.shiftImm(5, 0x00, rd, rs1, sh) }
func (a *Asm) Srai(rd, rs1 Reg, sh uint8) bool { return a.shiftImm(5, 0x20, rd, rs1, sh) }

func (a *Asm) Lui(rd Reg, imm20 int32) bool   { return a.utype(0x37, rd, imm20) }
func (a *Asm) Auipc(rd Reg, imm20 int32) bool { return a.utype(0x17, rd, imm20) }

// Pseudo-instruction helpers.
func (a *Asm) MovReg(rd, rs Reg) { _ = a.Addi(rd, rs, 0) }
func (a *Asm) Neg(rd, rs Reg)    { a.Sub(rd, Zero, rs) }
func (a *Asm) Not(rd, rs Reg)    { _ = a.Xori(rd, rs, -1) }
func (a *Asm) Seqz(rd, rs Reg)   { _ = a.Sltiu(rd, rs, 1) }
func (a *Asm) Snez(rd, rs Reg)   { a.Sltu(rd, Zero, rs) }
func (a *Asm) Sext8(rd, rs Reg) {
	_ = a.Slli(rd, rs, 24)
	_ = a.Srai(rd, rd, 24)
}
func (a *Asm) Sext16(rd, rs Reg) {
	_ = a.Slli(rd, rs, 16)
	_ = a.Srai(rd, rd, 16)
}

// MovImm32 materializes an arbitrary 32-bit bit pattern in at most two words.
func (a *Asm) MovImm32(rd Reg, val uint32) {
	s := int64(int32(val))
	if fitsSigned(s, 12) {
		_ = a.Addi(rd, Zero, int32(s))
		return
	}
	lo := s & 0xfff
	if lo >= 0x800 {
		lo -= 0x1000
	}
	hi := (s - lo) >> 12
	if hi == 1<<19 {
		hi = -(1 << 19)
	}
	_ = a.Lui(rd, int32(hi))
	if lo != 0 {
		_ = a.Addi(rd, rd, int32(lo))
	}
}
func (a *Asm) MovSigned32(rd Reg, val int32) { a.MovImm32(rd, uint32(val)) }

// RV32M.
func (a *Asm) Mul(rd, rs1, rs2 Reg)    { a.rtype(0x33, 0, 0x01, rd, rs1, rs2) }
func (a *Asm) Mulh(rd, rs1, rs2 Reg)   { a.rtype(0x33, 1, 0x01, rd, rs1, rs2) }
func (a *Asm) Mulhsu(rd, rs1, rs2 Reg) { a.rtype(0x33, 2, 0x01, rd, rs1, rs2) }
func (a *Asm) Mulhu(rd, rs1, rs2 Reg)  { a.rtype(0x33, 3, 0x01, rd, rs1, rs2) }
func (a *Asm) Div(rd, rs1, rs2 Reg)    { a.rtype(0x33, 4, 0x01, rd, rs1, rs2) }
func (a *Asm) Divu(rd, rs1, rs2 Reg)   { a.rtype(0x33, 5, 0x01, rd, rs1, rs2) }
func (a *Asm) Rem(rd, rs1, rs2 Reg)    { a.rtype(0x33, 6, 0x01, rd, rs1, rs2) }
func (a *Asm) Remu(rd, rs1, rs2 Reg)   { a.rtype(0x33, 7, 0x01, rd, rs1, rs2) }

// Integer memory operations.
func (a *Asm) Lb(rd, base Reg, off int32) bool  { return a.itype(0x03, 0, rd, base, off) }
func (a *Asm) Lh(rd, base Reg, off int32) bool  { return a.itype(0x03, 1, rd, base, off) }
func (a *Asm) Lw(rd, base Reg, off int32) bool  { return a.itype(0x03, 2, rd, base, off) }
func (a *Asm) Lbu(rd, base Reg, off int32) bool { return a.itype(0x03, 4, rd, base, off) }
func (a *Asm) Lhu(rd, base Reg, off int32) bool { return a.itype(0x03, 5, rd, base, off) }
func (a *Asm) Sb(src, base Reg, off int32) bool { return a.stype(0x23, 0, base, src, off) }
func (a *Asm) Sh(src, base Reg, off int32) bool { return a.stype(0x23, 1, base, src, off) }
func (a *Asm) Sw(src, base Reg, off int32) bool { return a.stype(0x23, 2, base, src, off) }

func encodeBranchImmediate(d int64) (uint32, bool) {
	if d&1 != 0 || !fitsSigned(d, 13) {
		return 0, false
	}
	u := uint32(d) & 0x1fff
	return (u>>12&1)<<31 | (u>>5&0x3f)<<25 | (u>>1&0xf)<<8 | (u>>11&1)<<7, true
}
func encodeJALImmediate(d int64) (uint32, bool) {
	if d&1 != 0 || !fitsSigned(d, 21) {
		return 0, false
	}
	u := uint32(d) & 0x1fffff
	return (u>>20&1)<<31 | (u>>1&0x3ff)<<21 | (u>>11&1)<<20 | (u>>12&0xff)<<12, true
}

func (a *Asm) Bcond(rs1, rs2 Reg, c Cond) int {
	at := a.Len()
	a.word(0x63 | (uint32(c)&7)<<12 | r(rs1)<<15 | r(rs2)<<20)
	return at
}
func (a *Asm) PatchBranch13(at, target int) bool {
	imm, ok := encodeBranchImmediate(int64(target - at))
	if !ok {
		return false
	}
	a.replaceWordFields(at, 0xfe000f80, imm)
	return true
}
func (a *Asm) Jal(rd Reg) int {
	at := a.Len()
	a.word(0x6f | r(rd)<<7)
	return at
}
func (a *Asm) PatchJAL21(at, target int) bool {
	imm, ok := encodeJALImmediate(int64(target - at))
	if !ok {
		return false
	}
	a.replaceWordFields(at, 0xfffff000, imm)
	return true
}
func (a *Asm) Jalr(rd, base Reg, off int32) bool { return a.itype(0x67, 0, rd, base, off) }
func (a *Asm) Ret()                              { _ = a.Jalr(Zero, RA, 0) }
func (a *Asm) Br(base Reg)                       { _ = a.Jalr(Zero, base, 0) }
func (a *Asm) Blr(base Reg)                      { _ = a.Jalr(RA, base, 0) }

func (a *Asm) FarJump(rd, scratch Reg) int {
	at := a.Len()
	_ = a.Auipc(scratch, 0)
	_ = a.Jalr(rd, scratch, 0)
	return at
}
func (a *Asm) FarCall(scratch Reg) int          { return a.FarJump(RA, scratch) }
func (a *Asm) PatchFarJump(at, target int) bool { return a.PatchAdr(at, target) }
func (a *Asm) FarBcond(rs1, rs2 Reg, c Cond, scratch Reg) int {
	at := a.Len()
	skip := a.Bcond(rs1, rs2, c.Invert())
	if !a.PatchBranch13(skip, skip+12) {
		panic("riscv32: internal far-branch skip out of range")
	}
	a.FarJump(Zero, scratch)
	return at
}
func (a *Asm) PatchFarBranch(at, target int) bool { return a.PatchFarJump(at+4, target) }
func (a *Asm) Adr(rd Reg) int {
	at := a.Len()
	_ = a.Auipc(rd, 0)
	_ = a.Addi(rd, rd, 0)
	return at
}
func (a *Asm) PatchAdr(at, target int) bool {
	d := int64(target - at)
	hi := (d + 0x800) >> 12
	lo := d - hi<<12
	if !fitsSigned(hi, 20) || !fitsSigned(lo, 12) {
		return false
	}
	a.replaceWordFields(at, 0xfffff000, (uint32(hi)&0xfffff)<<12)
	a.replaceWordFields(at+4, 0xfff00000, (uint32(lo)&0xfff)<<20)
	return true
}

func (a *Asm) Fence()  { a.word(0x0ff0000f) }
func (a *Asm) FenceI() { a.word(0x0000100f) }
func (a *Asm) Ecall()  { a.word(0x00000073) }
func (a *Asm) Ebreak() { a.word(0x00100073) }
func (a *Asm) Nop()    { a.word(0x00000013) }
func (a *Asm) Align16() {
	for len(a.B)%16 != 0 {
		a.Nop()
	}
}
