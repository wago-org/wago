// Package riscv64 is wago's RV64 instruction encoder — the RISC-V twin of
// src/core/encoder/arm64. Every base RV64 instruction is a fixed 32-bit
// little-endian word, so this package builds instructions from a base opcode
// OR-ed with range-checked R/I/S/B/U/J-format fields.
//
// The initial writer covers the scalar RV64G foundation needed by a future
// railshot backend: RV64I integer/control/memory operations, the M extension,
// PC-relative patching, constant materialization, and the system/fence
// instructions needed at the JIT boundary. Scalar F/D and A-extension methods
// live in asm2.go. Compressed and vector encodings are intentionally separate
// future layers: this writer always emits 4-byte instructions.
//
// This package only writes bytes. It knows nothing about the wasm operand stack,
// register allocation, the Go ABI, or wago's runtime conventions.
package riscv64

// Reg is a physical register number. Integer X0..X31 and floating-point F0..F31
// share this numeric type; the instruction method determines which register file
// the number names, matching the arm64 encoder's GPR/SIMD register overloading.
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

// Integer ABI aliases from the RISC-V ELF psABI. GP and TP are exposed so a
// backend can exclude them from allocation; their process/thread roles must be
// preserved across generated code.
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

// Floating-point register numbers and ABI aliases. They deliberately use Reg so
// scalar-FP methods have the same compact call shape as arm64's writer.
const (
	F0 Reg = iota
	F1
	F2
	F3
	F4
	F5
	F6
	F7
	F8
	F9
	F10
	F11
	F12
	F13
	F14
	F15
	F16
	F17
	F18
	F19
	F20
	F21
	F22
	F23
	F24
	F25
	F26
	F27
	F28
	F29
	F30
	F31
)

const (
	FT0  = F0
	FT1  = F1
	FT2  = F2
	FT3  = F3
	FT4  = F4
	FT5  = F5
	FT6  = F6
	FT7  = F7
	FS0  = F8
	FS1  = F9
	FA0  = F10
	FA1  = F11
	FA2  = F12
	FA3  = F13
	FA4  = F14
	FA5  = F15
	FA6  = F16
	FA7  = F17
	FS2  = F18
	FS3  = F19
	FS4  = F20
	FS5  = F21
	FS6  = F22
	FS7  = F23
	FS8  = F24
	FS9  = F25
	FS10 = F26
	FS11 = F27
	FT8  = F28
	FT9  = F29
	FT10 = F30
	FT11 = F31
)

// Cond is the funct3 field used by the six base conditional branches.
type Cond uint8

const (
	CondEQ  Cond = 0 // ==
	CondNE  Cond = 1 // !=
	CondLT  Cond = 4 // signed <
	CondGE  Cond = 5 // signed >=
	CondLTU Cond = 6 // unsigned <
	CondGEU Cond = 7 // unsigned >=
)

// Invert returns the branch condition true exactly when c is false. The base
// RISC-V branch encodings are paired by their low bit.
func (c Cond) Invert() Cond { return c ^ 1 }

// Asm accumulates encoded instruction words as little-endian bytes. Its zero
// value is ready to use, matching arm64.Asm.
type Asm struct {
	B []byte
}

// word appends one 32-bit instruction little-endian.
func (a *Asm) word(w uint32) {
	a.B = append(a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
}

// Len is the current code length in bytes and the offset of the next word.
func (a *Asm) Len() int { return len(a.B) }

// Grow reserves room for n more bytes without changing the emitted length.
func (a *Asm) Grow(n int) {
	if n <= 0 || cap(a.B)-len(a.B) >= n {
		return
	}
	b := make([]byte, len(a.B), len(a.B)+n)
	copy(b, a.B)
	a.B = b
}

// wordAt reads the 32-bit word at byte offset pos.
func (a *Asm) wordAt(pos int) uint32 {
	return uint32(a.B[pos]) | uint32(a.B[pos+1])<<8 | uint32(a.B[pos+2])<<16 | uint32(a.B[pos+3])<<24
}

// replaceWordFields replaces masked fields in the word at byte offset pos.
func (a *Asm) replaceWordFields(pos int, mask, fields uint32) {
	w := a.wordAt(pos)&^mask | fields&mask
	a.B[pos] = byte(w)
	a.B[pos+1] = byte(w >> 8)
	a.B[pos+2] = byte(w >> 16)
	a.B[pos+3] = byte(w >> 24)
}

// PatchU32 overwrites the raw 32-bit little-endian word at byte offset at. It is
// used for jump-table data as well as tests of patchable instruction fields.
func (a *Asm) PatchU32(at int, val uint32) {
	a.B[at] = byte(val)
	a.B[at+1] = byte(val >> 8)
	a.B[at+2] = byte(val >> 16)
	a.B[at+3] = byte(val >> 24)
}

func r(x Reg) uint32 { return uint32(x) & 31 }

func fitsSigned(v int64, bits uint) bool {
	min := -(int64(1) << (bits - 1))
	max := (int64(1) << (bits - 1)) - 1
	return v >= min && v <= max
}

// Instruction format helpers. Immediate helpers return false instead of
// truncating an unencodable value; no bytes are appended on failure.
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

// --- RV64I register operations ---

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

func (a *Asm) Addw(rd, rs1, rs2 Reg) { a.rtype(0x3b, 0, 0x00, rd, rs1, rs2) }
func (a *Asm) Subw(rd, rs1, rs2 Reg) { a.rtype(0x3b, 0, 0x20, rd, rs1, rs2) }
func (a *Asm) Sllw(rd, rs1, rs2 Reg) { a.rtype(0x3b, 1, 0x00, rd, rs1, rs2) }
func (a *Asm) Srlw(rd, rs1, rs2 Reg) { a.rtype(0x3b, 5, 0x00, rd, rs1, rs2) }
func (a *Asm) Sraw(rd, rs1, rs2 Reg) { a.rtype(0x3b, 5, 0x20, rd, rs1, rs2) }

// Width-named aliases keep future railshot ports close to the arm64 writer.
func (a *Asm) Add64(rd, rs1, rs2 Reg) { a.Add(rd, rs1, rs2) }
func (a *Asm) Sub64(rd, rs1, rs2 Reg) { a.Sub(rd, rs1, rs2) }
func (a *Asm) Add32(rd, rs1, rs2 Reg) { a.Addw(rd, rs1, rs2) }
func (a *Asm) Sub32(rd, rs1, rs2 Reg) { a.Subw(rd, rs1, rs2) }
func (a *Asm) And64(rd, rs1, rs2 Reg) { a.And(rd, rs1, rs2) }
func (a *Asm) Orr64(rd, rs1, rs2 Reg) { a.Or(rd, rs1, rs2) }
func (a *Asm) Eor64(rd, rs1, rs2 Reg) { a.Xor(rd, rs1, rs2) }

// --- RV64I immediate operations ---

func (a *Asm) Addi(rd, rs1 Reg, imm int32) bool  { return a.itype(0x13, 0, rd, rs1, imm) }
func (a *Asm) Slti(rd, rs1 Reg, imm int32) bool  { return a.itype(0x13, 2, rd, rs1, imm) }
func (a *Asm) Sltiu(rd, rs1 Reg, imm int32) bool { return a.itype(0x13, 3, rd, rs1, imm) }
func (a *Asm) Xori(rd, rs1 Reg, imm int32) bool  { return a.itype(0x13, 4, rd, rs1, imm) }
func (a *Asm) Ori(rd, rs1 Reg, imm int32) bool   { return a.itype(0x13, 6, rd, rs1, imm) }
func (a *Asm) Andi(rd, rs1 Reg, imm int32) bool  { return a.itype(0x13, 7, rd, rs1, imm) }
func (a *Asm) Addiw(rd, rs1 Reg, imm int32) bool { return a.itype(0x1b, 0, rd, rs1, imm) }

func (a *Asm) AddImm64(rd, rs1 Reg, imm int32) bool { return a.Addi(rd, rs1, imm) }
func (a *Asm) AddImm32(rd, rs1 Reg, imm int32) bool { return a.Addiw(rd, rs1, imm) }
func (a *Asm) SubImm64(rd, rs1 Reg, imm int32) bool {
	if imm == -2048 {
		return false
	}
	return a.Addi(rd, rs1, -imm)
}
func (a *Asm) SubImm32(rd, rs1 Reg, imm int32) bool {
	if imm == -2048 {
		return false
	}
	return a.Addiw(rd, rs1, -imm)
}
func (a *Asm) AndImm64(rd, rs1 Reg, imm int32) bool { return a.Andi(rd, rs1, imm) }
func (a *Asm) OrrImm64(rd, rs1 Reg, imm int32) bool { return a.Ori(rd, rs1, imm) }
func (a *Asm) EorImm64(rd, rs1 Reg, imm int32) bool { return a.Xori(rd, rs1, imm) }

func (a *Asm) shiftImm(opcode, funct3, funct6 uint32, rd, rs1 Reg, sh uint8, bits uint8) bool {
	if sh >= bits {
		return false
	}
	imm := funct6<<6 | uint32(sh)
	a.word(opcode | r(rd)<<7 | funct3<<12 | r(rs1)<<15 | imm<<20)
	return true
}

func (a *Asm) Slli(rd, rs1 Reg, sh uint8) bool { return a.shiftImm(0x13, 1, 0x00, rd, rs1, sh, 64) }
func (a *Asm) Srli(rd, rs1 Reg, sh uint8) bool { return a.shiftImm(0x13, 5, 0x00, rd, rs1, sh, 64) }
func (a *Asm) Srai(rd, rs1 Reg, sh uint8) bool { return a.shiftImm(0x13, 5, 0x10, rd, rs1, sh, 64) }

func (a *Asm) shiftImmW(funct3, funct7 uint32, rd, rs1 Reg, sh uint8) bool {
	if sh >= 32 {
		return false
	}
	a.word(0x1b | r(rd)<<7 | funct3<<12 | r(rs1)<<15 | uint32(sh)<<20 | funct7<<25)
	return true
}

func (a *Asm) Slliw(rd, rs1 Reg, sh uint8) bool { return a.shiftImmW(1, 0x00, rd, rs1, sh) }
func (a *Asm) Srliw(rd, rs1 Reg, sh uint8) bool { return a.shiftImmW(5, 0x00, rd, rs1, sh) }
func (a *Asm) Sraiw(rd, rs1 Reg, sh uint8) bool { return a.shiftImmW(5, 0x20, rd, rs1, sh) }

func (a *Asm) LslImm64(rd, rs1 Reg, sh uint8) bool { return a.Slli(rd, rs1, sh) }
func (a *Asm) LsrImm64(rd, rs1 Reg, sh uint8) bool { return a.Srli(rd, rs1, sh) }
func (a *Asm) AsrImm64(rd, rs1 Reg, sh uint8) bool { return a.Srai(rd, rs1, sh) }
func (a *Asm) LslImm32(rd, rs1 Reg, sh uint8) bool { return a.Slliw(rd, rs1, sh) }
func (a *Asm) LsrImm32(rd, rs1 Reg, sh uint8) bool { return a.Srliw(rd, rs1, sh) }
func (a *Asm) AsrImm32(rd, rs1 Reg, sh uint8) bool { return a.Sraiw(rd, rs1, sh) }

func (a *Asm) Lui(rd Reg, imm20 int32) bool   { return a.utype(0x37, rd, imm20) }
func (a *Asm) Auipc(rd Reg, imm20 int32) bool { return a.utype(0x17, rd, imm20) }

// --- Integer pseudo-instruction helpers ---

func (a *Asm) MovReg64(rd, rs Reg) { a.Addi(rd, rs, 0) }
func (a *Asm) MovReg32(rd, rs Reg) { a.Addiw(rd, rs, 0) }
func (a *Asm) Neg64(rd, rs Reg)    { a.Sub(rd, Zero, rs) }
func (a *Asm) Neg32(rd, rs Reg)    { a.Subw(rd, Zero, rs) }
func (a *Asm) Not(rd, rs Reg)      { a.Xori(rd, rs, -1) }
func (a *Asm) Seqz(rd, rs Reg)     { a.Sltiu(rd, rs, 1) }
func (a *Asm) Snez(rd, rs Reg)     { a.Sltu(rd, Zero, rs) }
func (a *Asm) Sext32(rd, rs Reg)   { a.Addiw(rd, rs, 0) }

// Zext32 clears the high half without requiring the optional Zba extension.
func (a *Asm) Zext32(rd, rs Reg) {
	a.Slli(rd, rs, 32)
	a.Srli(rd, rd, 32)
}

func (a *Asm) Sext8(rd, rs Reg) {
	a.Slli(rd, rs, 56)
	a.Srai(rd, rd, 56)
}

func (a *Asm) Sext16(rd, rs Reg) {
	a.Slli(rd, rs, 48)
	a.Srai(rd, rd, 48)
}

// MovImm64 materializes an arbitrary 64-bit bit pattern. It uses ADDI for a
// signed 12-bit value, LUI+ADDIW for a signed 32-bit value, and recursively
// builds larger values from signed base-4096 chunks. Runs of zero low chunks are
// folded into one larger SLLI. The sequence is deterministic and at most eight
// instructions for any 64-bit value.
func (a *Asm) MovImm64(rd Reg, val uint64) { a.movImmSigned64(rd, int64(val)) }

func signedLow12(val int64) int64 {
	lo := val & 0xfff
	if lo >= 0x800 {
		lo -= 0x1000
	}
	return lo
}

func (a *Asm) movImmSigned64(rd Reg, val int64) {
	if fitsSigned(val, 12) {
		a.Addi(rd, Zero, int32(val))
		return
	}
	if fitsSigned(val, 32) {
		lo := signedLow12(val)
		hi := (val - lo) >> 12
		if hi == 0 {
			a.Addi(rd, Zero, int32(lo))
			return
		}
		// The rounded high part of a positive signed-32 value can be +2^19.
		// LUI encodes that field as -2^19; ADDIW then truncates and restores
		// the intended signed 32-bit result.
		if hi == 1<<19 {
			hi = -(1 << 19)
		}
		a.Lui(rd, int32(hi))
		if lo != 0 {
			a.Addiw(rd, rd, int32(lo))
		}
		return
	}

	lo := signedLow12(val)
	hi := (val - lo) >> 12
	shift := uint8(12)
	for hi != 0 && hi&0xfff == 0 && shift <= 48 {
		hi >>= 12
		shift += 12
	}
	a.movImmSigned64(rd, hi)
	a.Slli(rd, rd, shift)
	if lo != 0 {
		a.Addi(rd, rd, int32(lo))
	}
}

// MovImm32 materializes the zero-extended 32-bit pattern used by wasm i32
// constants. SextImm32 is available when a signed canonical value is preferred.
func (a *Asm) MovImm32(rd Reg, val int32)    { a.MovImm64(rd, uint64(uint32(val))) }
func (a *Asm) MovSigned32(rd Reg, val int32) { a.movImmSigned64(rd, int64(val)) }

// --- RV64M multiply/divide ---

func (a *Asm) Mul(rd, rs1, rs2 Reg)    { a.rtype(0x33, 0, 0x01, rd, rs1, rs2) }
func (a *Asm) Mulh(rd, rs1, rs2 Reg)   { a.rtype(0x33, 1, 0x01, rd, rs1, rs2) }
func (a *Asm) Mulhsu(rd, rs1, rs2 Reg) { a.rtype(0x33, 2, 0x01, rd, rs1, rs2) }
func (a *Asm) Mulhu(rd, rs1, rs2 Reg)  { a.rtype(0x33, 3, 0x01, rd, rs1, rs2) }
func (a *Asm) Div(rd, rs1, rs2 Reg)    { a.rtype(0x33, 4, 0x01, rd, rs1, rs2) }
func (a *Asm) Divu(rd, rs1, rs2 Reg)   { a.rtype(0x33, 5, 0x01, rd, rs1, rs2) }
func (a *Asm) Rem(rd, rs1, rs2 Reg)    { a.rtype(0x33, 6, 0x01, rd, rs1, rs2) }
func (a *Asm) Remu(rd, rs1, rs2 Reg)   { a.rtype(0x33, 7, 0x01, rd, rs1, rs2) }

func (a *Asm) Mulw(rd, rs1, rs2 Reg)  { a.rtype(0x3b, 0, 0x01, rd, rs1, rs2) }
func (a *Asm) Divw(rd, rs1, rs2 Reg)  { a.rtype(0x3b, 4, 0x01, rd, rs1, rs2) }
func (a *Asm) Divuw(rd, rs1, rs2 Reg) { a.rtype(0x3b, 5, 0x01, rd, rs1, rs2) }
func (a *Asm) Remw(rd, rs1, rs2 Reg)  { a.rtype(0x3b, 6, 0x01, rd, rs1, rs2) }
func (a *Asm) Remuw(rd, rs1, rs2 Reg) { a.rtype(0x3b, 7, 0x01, rd, rs1, rs2) }

func (a *Asm) Mul64(rd, rs1, rs2 Reg)  { a.Mul(rd, rs1, rs2) }
func (a *Asm) Mul32(rd, rs1, rs2 Reg)  { a.Mulw(rd, rs1, rs2) }
func (a *Asm) Sdiv64(rd, rs1, rs2 Reg) { a.Div(rd, rs1, rs2) }
func (a *Asm) Udiv64(rd, rs1, rs2 Reg) { a.Divu(rd, rs1, rs2) }
func (a *Asm) Sdiv32(rd, rs1, rs2 Reg) { a.Divw(rd, rs1, rs2) }
func (a *Asm) Udiv32(rd, rs1, rs2 Reg) { a.Divuw(rd, rs1, rs2) }

// --- Integer loads / stores ---

func (a *Asm) Lb(rd, base Reg, off int32) bool  { return a.itype(0x03, 0, rd, base, off) }
func (a *Asm) Lh(rd, base Reg, off int32) bool  { return a.itype(0x03, 1, rd, base, off) }
func (a *Asm) Lw(rd, base Reg, off int32) bool  { return a.itype(0x03, 2, rd, base, off) }
func (a *Asm) Ld(rd, base Reg, off int32) bool  { return a.itype(0x03, 3, rd, base, off) }
func (a *Asm) Lbu(rd, base Reg, off int32) bool { return a.itype(0x03, 4, rd, base, off) }
func (a *Asm) Lhu(rd, base Reg, off int32) bool { return a.itype(0x03, 5, rd, base, off) }
func (a *Asm) Lwu(rd, base Reg, off int32) bool { return a.itype(0x03, 6, rd, base, off) }

func (a *Asm) Sb(src, base Reg, off int32) bool { return a.stype(0x23, 0, base, src, off) }
func (a *Asm) Sh(src, base Reg, off int32) bool { return a.stype(0x23, 1, base, src, off) }
func (a *Asm) Sw(src, base Reg, off int32) bool { return a.stype(0x23, 2, base, src, off) }
func (a *Asm) Sd(src, base Reg, off int32) bool { return a.stype(0x23, 3, base, src, off) }

func (a *Asm) Load64(dst, base Reg, off int32) bool  { return a.Ld(dst, base, off) }
func (a *Asm) Load32(dst, base Reg, off int32) bool  { return a.Lw(dst, base, off) }
func (a *Asm) Load32U(dst, base Reg, off int32) bool { return a.Lwu(dst, base, off) }
func (a *Asm) Store64(src, base Reg, off int32) bool { return a.Sd(src, base, off) }
func (a *Asm) Store32(src, base Reg, off int32) bool { return a.Sw(src, base, off) }

// --- Branches / jumps / PC-relative patching ---

// Bcond emits a conditional branch with a zero displacement and returns its byte
// offset. Patch it with PatchBranch13 once the target is known.
func (a *Asm) Bcond(rs1, rs2 Reg, c Cond) int {
	at := a.Len()
	a.word(0x63 | (uint32(c)&7)<<12 | r(rs1)<<15 | r(rs2)<<20)
	return at
}

func (a *Asm) Beq(rs1, rs2 Reg) int  { return a.Bcond(rs1, rs2, CondEQ) }
func (a *Asm) Bne(rs1, rs2 Reg) int  { return a.Bcond(rs1, rs2, CondNE) }
func (a *Asm) Blt(rs1, rs2 Reg) int  { return a.Bcond(rs1, rs2, CondLT) }
func (a *Asm) Bge(rs1, rs2 Reg) int  { return a.Bcond(rs1, rs2, CondGE) }
func (a *Asm) Bltu(rs1, rs2 Reg) int { return a.Bcond(rs1, rs2, CondLTU) }
func (a *Asm) Bgeu(rs1, rs2 Reg) int { return a.Bcond(rs1, rs2, CondGEU) }

// PatchBranch13 fills the split 13-bit signed displacement of a base branch.
// Its byte range is [-4096,4094]. Repatching is supported.
func (a *Asm) PatchBranch13(at, target int) bool {
	imm, ok := encodeBranchImmediate(int64(target - at))
	if !ok {
		return false
	}
	a.replaceWordFields(at, 0xfe000f80, imm)
	return true
}

// Jal emits JAL rd with a zero displacement and returns its byte offset. Use
// Zero for an unconditional jump and RA for a direct call.
func (a *Asm) Jal(rd Reg) int {
	at := a.Len()
	a.word(0x6f | r(rd)<<7)
	return at
}

func (a *Asm) Jump() int { return a.Jal(Zero) }
func (a *Asm) Call() int { return a.Jal(RA) }

// PatchJAL21 fills JAL's split 21-bit signed displacement. Its byte range is
// [-1 MiB,+1 MiB-2]. Repatching is supported.
func (a *Asm) PatchJAL21(at, target int) bool {
	imm, ok := encodeJALImmediate(int64(target - at))
	if !ok {
		return false
	}
	a.replaceWordFields(at, 0xfffff000, imm)
	return true
}

func (a *Asm) Jalr(rd, base Reg, off int32) bool { return a.itype(0x67, 0, rd, base, off) }
func (a *Asm) Ret()                              { a.Jalr(Zero, RA, 0) }
func (a *Asm) Br(base Reg)                       { a.Jalr(Zero, base, 0) }
func (a *Asm) Blr(base Reg)                      { a.Jalr(RA, base, 0) }

// FarJump reserves an AUIPC+JALR pair. rd selects jump (Zero) or call (RA)
// linkage, and scratch receives the PC-relative high part. PatchFarJump fills
// the pair once the target is known. Unlike JAL, this reaches approximately
// ±2 GiB and is the normal direct-transfer shape for large generated modules.
func (a *Asm) FarJump(rd, scratch Reg) int {
	at := a.Len()
	a.Auipc(scratch, 0)
	a.Jalr(rd, scratch, 0)
	return at
}

func (a *Asm) FarCall(scratch Reg) int { return a.FarJump(RA, scratch) }

func (a *Asm) PatchFarJump(at, target int) bool { return a.PatchAdr(at, target) }

// FarBcond emits a fixed-size long conditional transfer:
//
//	b.<inverse> rs1,rs2,+12
//	auipc       scratch,0
//	jalr        x0,scratch,0
//
// The short inverse branch skips the far jump when the requested condition is
// false. PatchFarBranch patches the AUIPC+JALR pair. This shape avoids the base
// branch instruction's ±4 KiB range limit without moving already-emitted code.
func (a *Asm) FarBcond(rs1, rs2 Reg, c Cond, scratch Reg) int {
	at := a.Len()
	skip := a.Bcond(rs1, rs2, c.Invert())
	if !a.PatchBranch13(skip, skip+12) {
		panic("riscv64: internal far-branch skip is out of range")
	}
	a.FarJump(Zero, scratch)
	return at
}

func (a *Asm) PatchFarBranch(at, target int) bool { return a.PatchFarJump(at+4, target) }

// Adr reserves an AUIPC+ADDI pair that computes a PC-relative address into rd.
// PatchAdr fills both immediates once the target byte offset is known.
func (a *Asm) Adr(rd Reg) int {
	at := a.Len()
	a.Auipc(rd, 0)
	a.Addi(rd, rd, 0)
	return at
}

// PatchAdr patches an AUIPC+I-type pair at at/at+4. It is also suitable for an
// AUIPC+JALR far call/jump pair. The rounded high 20-bit part must fit signed
// 20 bits; the exact reachable interval is therefore slightly asymmetric around
// the nominal ±2 GiB boundary.
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

// --- Memory ordering and environment ---

// Fence emits FENCE iorw,iorw, the full predecessor/successor ordering used by
// Go's RISC-V assembler for its canonical FENCE pseudo-instruction.
func (a *Asm) Fence()  { a.word(0x0ff0000f) }
func (a *Asm) FenceI() { a.word(0x0000100f) }
func (a *Asm) Ecall()  { a.word(0x00000073) }
func (a *Asm) Ebreak() { a.word(0x00100073) }

// Nop is the canonical ADDI x0,x0,0 no-op.
func (a *Asm) Nop() { a.word(0x00000013) }

// Align16 pads with NOPs to a 16-byte boundary.
func (a *Asm) Align16() {
	for len(a.B)%16 != 0 {
		a.Nop()
	}
}
