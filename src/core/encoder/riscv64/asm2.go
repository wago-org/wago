package riscv64

// This file extends the base RV64I/M writer with the remaining scalar pieces of
// RV64G: F/D floating point, A-extension atomics, and Zicsr operations. As in the
// arm64 writer, methods are thin typed field setters over fixed 32-bit opcode
// words; semantic lowering (NaN behavior, traps, register allocation) belongs in
// the future railshot backend.

// RoundingMode is the rm field used by RISC-V floating-point instructions.
type RoundingMode uint8

const (
	RoundNearestEven RoundingMode = 0 // RNE
	RoundTowardZero  RoundingMode = 1 // RTZ
	RoundDown        RoundingMode = 2 // RDN
	RoundUp          RoundingMode = 3 // RUP
	RoundNearestMax  RoundingMode = 4 // RMM: ties to maximum magnitude
	RoundDynamic     RoundingMode = 7 // DYN: use frm CSR
)

func (rm RoundingMode) valid() bool { return rm <= RoundNearestMax || rm == RoundDynamic }

func fpBase(f64 bool, single, double uint32) uint32 {
	if f64 {
		return double
	}
	return single
}

func (a *Asm) fpR(funct7, funct3 uint32, rd, rs1, rs2 Reg) {
	a.rtype(0x53, funct3, funct7, rd, rs1, rs2)
}

func (a *Asm) fpRound(funct7 uint32, rd, rs1, rs2 Reg, rm RoundingMode) bool {
	if !rm.valid() {
		return false
	}
	a.fpR(funct7, uint32(rm), rd, rs1, rs2)
	return true
}

// --- F/D scalar arithmetic ---

func (a *Asm) Fadd(rd, rs1, rs2 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpRound(fpBase(f64, 0x00, 0x01), rd, rs1, rs2, rm)
}
func (a *Asm) Fsub(rd, rs1, rs2 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpRound(fpBase(f64, 0x04, 0x05), rd, rs1, rs2, rm)
}
func (a *Asm) Fmul(rd, rs1, rs2 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpRound(fpBase(f64, 0x08, 0x09), rd, rs1, rs2, rm)
}
func (a *Asm) Fdiv(rd, rs1, rs2 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpRound(fpBase(f64, 0x0c, 0x0d), rd, rs1, rs2, rm)
}
func (a *Asm) Fsqrt(rd, rs1 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpRound(fpBase(f64, 0x2c, 0x2d), rd, rs1, F0, rm)
}

// Fmin/Fmax have a fixed funct3 rather than a rounding-mode field. Their native
// NaN and signed-zero behavior is an ISA primitive, not a claim that one
// instruction is sufficient for WebAssembly min/max semantics.
func (a *Asm) Fmin(rd, rs1, rs2 Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x14, 0x15), 0, rd, rs1, rs2)
}
func (a *Asm) Fmax(rd, rs1, rs2 Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x14, 0x15), 1, rd, rs1, rs2)
}

func (a *Asm) Fsgnj(rd, rs1, rs2 Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x10, 0x11), 0, rd, rs1, rs2)
}
func (a *Asm) Fsgnjn(rd, rs1, rs2 Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x10, 0x11), 1, rd, rs1, rs2)
}
func (a *Asm) Fsgnjx(rd, rs1, rs2 Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x10, 0x11), 2, rd, rs1, rs2)
}

// Move/abs/neg aliases are the canonical FSGNJ-family pseudo-instructions.
func (a *Asm) FmovReg(rd, rs Reg, f64 bool) { a.Fsgnj(rd, rs, rs, f64) }
func (a *Asm) Fabs(rd, rs Reg, f64 bool)    { a.Fsgnjx(rd, rs, rs, f64) }
func (a *Asm) Fneg(rd, rs Reg, f64 bool)    { a.Fsgnjn(rd, rs, rs, f64) }

// --- Fused multiply-add ---

func (a *Asm) fpFused(opcode uint32, rd, rs1, rs2, rs3 Reg, f64 bool, rm RoundingMode) bool {
	if !rm.valid() {
		return false
	}
	fmtField := uint32(0)
	if f64 {
		fmtField = 1
	}
	a.word(opcode | r(rd)<<7 | uint32(rm)<<12 | r(rs1)<<15 | r(rs2)<<20 | fmtField<<25 | r(rs3)<<27)
	return true
}

func (a *Asm) Fmadd(rd, rs1, rs2, rs3 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpFused(0x43, rd, rs1, rs2, rs3, f64, rm)
}
func (a *Asm) Fmsub(rd, rs1, rs2, rs3 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpFused(0x47, rd, rs1, rs2, rs3, f64, rm)
}
func (a *Asm) Fnmsub(rd, rs1, rs2, rs3 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpFused(0x4b, rd, rs1, rs2, rs3, f64, rm)
}
func (a *Asm) Fnmadd(rd, rs1, rs2, rs3 Reg, f64 bool, rm RoundingMode) bool {
	return a.fpFused(0x4f, rd, rs1, rs2, rs3, f64, rm)
}

// --- Floating-point comparisons, classification, and bit moves ---

func (a *Asm) Fle(rd, rs1, rs2 Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x50, 0x51), 0, rd, rs1, rs2)
}
func (a *Asm) Flt(rd, rs1, rs2 Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x50, 0x51), 1, rd, rs1, rs2)
}
func (a *Asm) Feq(rd, rs1, rs2 Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x50, 0x51), 2, rd, rs1, rs2)
}
func (a *Asm) Fclass(rd, rs Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x70, 0x71), 1, rd, rs, F0)
}

// FmvToGPR and FmvFromGPR reinterpret scalar bits. The single-precision
// FMV.X.W form sign-extends bit 31 into the upper XLEN bits; consumers that need
// a zero-extended uint32 must clear the high half explicitly.
func (a *Asm) FmvToGPR(rd, rs Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x70, 0x71), 0, rd, rs, F0)
}
func (a *Asm) FmvFromGPR(rd, rs Reg, f64 bool) {
	a.fpR(fpBase(f64, 0x78, 0x79), 0, rd, rs, F0)
}

// --- Floating-point conversions ---

// FcvtFloatToInt converts a scalar float to W/WU/L/LU. dst64 chooses L/LU;
// unsigned chooses the U form. WebAssembly trapping/saturating range checks are
// backend responsibilities around this primitive.
func (a *Asm) FcvtFloatToInt(rd, rs Reg, f64src, unsigned, dst64 bool, rm RoundingMode) bool {
	rs2 := Reg(0) // W
	if unsigned {
		rs2 = 1 // WU
	}
	if dst64 {
		rs2 += 2 // L/LU
	}
	return a.fpRound(fpBase(f64src, 0x60, 0x61), rd, rs, rs2, rm)
}

// FcvtIntToFloat converts W/WU/L/LU to S/D.
func (a *Asm) FcvtIntToFloat(rd, rs Reg, f64dst, unsigned, src64 bool, rm RoundingMode) bool {
	rs2 := Reg(0) // W
	if unsigned {
		rs2 = 1 // WU
	}
	if src64 {
		rs2 += 2 // L/LU
	}
	return a.fpRound(fpBase(f64dst, 0x68, 0x69), rd, rs, rs2, rm)
}

func (a *Asm) FcvtS2D(rd, rs Reg, rm RoundingMode) bool {
	return a.fpRound(0x21, rd, rs, F0, rm)
}
func (a *Asm) FcvtD2S(rd, rs Reg, rm RoundingMode) bool {
	return a.fpRound(0x20, rd, rs, F1, rm)
}

// --- Floating-point loads / stores ---

func (a *Asm) Flw(dst, base Reg, off int32) bool { return a.itype(0x07, 2, dst, base, off) }
func (a *Asm) Fld(dst, base Reg, off int32) bool { return a.itype(0x07, 3, dst, base, off) }
func (a *Asm) Fsw(src, base Reg, off int32) bool { return a.stype(0x27, 2, base, src, off) }
func (a *Asm) Fsd(src, base Reg, off int32) bool { return a.stype(0x27, 3, base, src, off) }

func (a *Asm) FLoad(dst, base Reg, off int32, f64 bool) bool {
	if f64 {
		return a.Fld(dst, base, off)
	}
	return a.Flw(dst, base, off)
}
func (a *Asm) FStore(src, base Reg, off int32, f64 bool) bool {
	if f64 {
		return a.Fsd(src, base, off)
	}
	return a.Fsw(src, base, off)
}

// --- A extension atomics ---

// MemoryOrder maps directly to the aq/rl bits: bit 1 is aq, bit 0 is rl.
type MemoryOrder uint8

const (
	OrderRelaxed MemoryOrder = iota
	OrderRelease
	OrderAcquire
	OrderAcquireRelease
)

func (a *Asm) atomic(funct5 uint32, rd, base, src Reg, wide64 bool, order MemoryOrder) {
	funct3 := uint32(2) // .W
	if wide64 {
		funct3 = 3 // .D
	}
	a.word(0x2f | r(rd)<<7 | funct3<<12 | r(base)<<15 | r(src)<<20 |
		(uint32(order)&3)<<25 | (funct5&0x1f)<<27)
}

func (a *Asm) Lr32(rd, base Reg, order MemoryOrder) { a.atomic(0x02, rd, base, Zero, false, order) }
func (a *Asm) Lr64(rd, base Reg, order MemoryOrder) { a.atomic(0x02, rd, base, Zero, true, order) }
func (a *Asm) Sc32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x03, rd, base, src, false, order)
}
func (a *Asm) Sc64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x03, rd, base, src, true, order)
}

func (a *Asm) AmoSwap32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x01, rd, base, src, false, order)
}
func (a *Asm) AmoSwap64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x01, rd, base, src, true, order)
}
func (a *Asm) AmoAdd32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x00, rd, base, src, false, order)
}
func (a *Asm) AmoAdd64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x00, rd, base, src, true, order)
}
func (a *Asm) AmoXor32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x04, rd, base, src, false, order)
}
func (a *Asm) AmoXor64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x04, rd, base, src, true, order)
}
func (a *Asm) AmoAnd32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x0c, rd, base, src, false, order)
}
func (a *Asm) AmoAnd64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x0c, rd, base, src, true, order)
}
func (a *Asm) AmoOr32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x08, rd, base, src, false, order)
}
func (a *Asm) AmoOr64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x08, rd, base, src, true, order)
}
func (a *Asm) AmoMin32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x10, rd, base, src, false, order)
}
func (a *Asm) AmoMin64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x10, rd, base, src, true, order)
}
func (a *Asm) AmoMax32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x14, rd, base, src, false, order)
}
func (a *Asm) AmoMax64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x14, rd, base, src, true, order)
}
func (a *Asm) AmoMinu32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x18, rd, base, src, false, order)
}
func (a *Asm) AmoMinu64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x18, rd, base, src, true, order)
}
func (a *Asm) AmoMaxu32(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x1c, rd, base, src, false, order)
}
func (a *Asm) AmoMaxu64(rd, base, src Reg, order MemoryOrder) {
	a.atomic(0x1c, rd, base, src, true, order)
}

// --- Zicsr ---

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

func (a *Asm) Csrrw(rd Reg, csr uint16, rs Reg) { a.csr(1, rd, csr, rs) }
func (a *Asm) Csrrs(rd Reg, csr uint16, rs Reg) { a.csr(2, rd, csr, rs) }
func (a *Asm) Csrrc(rd Reg, csr uint16, rs Reg) { a.csr(3, rd, csr, rs) }
func (a *Asm) Csrrwi(rd Reg, csr uint16, zimm uint8) bool {
	return a.csri(5, rd, csr, zimm)
}
func (a *Asm) Csrrsi(rd Reg, csr uint16, zimm uint8) bool {
	return a.csri(6, rd, csr, zimm)
}
func (a *Asm) Csrrci(rd Reg, csr uint16, zimm uint8) bool {
	return a.csri(7, rd, csr, zimm)
}
