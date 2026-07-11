// Package arm64 is wago's AArch64 (arm64) instruction encoder — the arm64 twin of
// src/core/encoder/amd64. Every A64 instruction is a fixed 32-bit little-endian
// word, so unlike the x86 encoder (variable-length REX/ModRM/SIB byte-smashing)
// this package builds each instruction from a constexpr-style base opcode word
// OR-ed with range-checked bit-fields (register numbers, scaled immediates,
// bitmask immediates, branch displacements). Base opcode words are cross-checked
// against clang's integrated assembler + llvm-objdump in asm_test.go; the
// approach mirrors WARP's warp/src/core/compiler/backend/aarch64 encoding table.
//
// This package only encodes bytes; it knows nothing about the wasm operand stack
// or register allocation — the railshot backend's arm64 emit files drive it, the
// same way they drive the amd64 encoder.
package arm64

// Reg is a physical register number. GPRs are 0..30 (X0..X30 / W0..W30); 31 is
// the zero register (XZR/WZR) in most encodings and the stack pointer (SP) in a
// few (add/sub-immediate, load/store base) — the caller picks the meaning by
// which method it calls. SIMD/FP registers V0..V31 reuse 0..31 and are
// disambiguated by the (future) float/vector methods, exactly like the amd64
// encoder overloads XMM onto the same numeric space.
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
	X29 // frame pointer (FP) by convention
	X30 // link register (LR)
	XZR // zero register / SP depending on instruction (encoded as 31)
)

// Role aliases (AAPCS64 / convention).
const (
	FP = X29
	LR = X30
	SP = XZR // 31 means SP in add/sub-imm and load/store base position
	ZR = XZR
)

// Cond is an AArch64 condition code (the 4-bit field used by B.cond / CSEL /
// CSINC). Values are the architectural encodings.
type Cond uint8

const (
	CondEQ Cond = 0x0 // ==
	CondNE Cond = 0x1 // !=
	CondCS Cond = 0x2 // unsigned >=  (HS)
	CondCC Cond = 0x3 // unsigned <   (LO)
	CondMI Cond = 0x4 // negative
	CondPL Cond = 0x5 // >= 0
	CondVS Cond = 0x6 // overflow set
	CondVC Cond = 0x7 // overflow clear
	CondHI Cond = 0x8 // unsigned >
	CondLS Cond = 0x9 // unsigned <=
	CondGE Cond = 0xA // signed >=
	CondLT Cond = 0xB // signed <
	CondGT Cond = 0xC // signed >
	CondLE Cond = 0xD // signed <=
	CondAL Cond = 0xE // always
	CondNV Cond = 0xF
)

// Invert returns the condition true exactly when c is false. A64 condition codes
// (like x86) are paired by their low bit.
func (c Cond) Invert() Cond { return c ^ 1 }

// Asm accumulates encoded instruction words as little-endian bytes. Its zero
// value is ready to use. Mirrors amd64.Asm{ B []byte }.
type Asm struct {
	B            []byte
	DenseIdxDisp bool // prefer ADD base,index + immediate-offset load/store
}

// word appends one 32-bit instruction little-endian.
func (a *Asm) word(w uint32) {
	a.B = append(a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
}

// Len is the current code length in bytes (also the offset of the next word).
func (a *Asm) Len() int { return len(a.B) }

// wordAt reads the 32-bit word at byte offset pos.
func (a *Asm) wordAt(pos int) uint32 {
	return uint32(a.B[pos]) | uint32(a.B[pos+1])<<8 | uint32(a.B[pos+2])<<16 | uint32(a.B[pos+3])<<24
}

// patchWord OR-s extra into the word at byte offset pos (used to fill a branch
// displacement field left zero by a placeholder emitter).
func (a *Asm) patchWord(pos int, extra uint32) {
	w := a.wordAt(pos) | extra
	a.B[pos] = byte(w)
	a.B[pos+1] = byte(w >> 8)
	a.B[pos+2] = byte(w >> 16)
	a.B[pos+3] = byte(w >> 24)
}

func r(x Reg) uint32 { return uint32(x) & 31 }

// --- Data-processing (shifted register), shift = LSL #0 ---

func (a *Asm) addSubReg(base uint32, rd, rn, rm Reg) {
	a.word(base | r(rm)<<16 | r(rn)<<5 | r(rd))
}

func (a *Asm) Add64(rd, rn, rm Reg)  { a.addSubReg(0x8B000000, rd, rn, rm) }
func (a *Asm) Add32(rd, rn, rm Reg)  { a.addSubReg(0x0B000000, rd, rn, rm) }
func (a *Asm) Sub64(rd, rn, rm Reg)  { a.addSubReg(0xCB000000, rd, rn, rm) }
func (a *Asm) Sub32(rd, rn, rm Reg)  { a.addSubReg(0x4B000000, rd, rn, rm) }
func (a *Asm) Adds64(rd, rn, rm Reg) { a.addSubReg(0xAB000000, rd, rn, rm) }
func (a *Asm) Subs64(rd, rn, rm Reg) { a.addSubReg(0xEB000000, rd, rn, rm) }

// CmpReg64 is SUBS XZR, Rn, Rm — sets NZCV, discards the result.
func (a *Asm) CmpReg64(rn, rm Reg) { a.addSubReg(0xEB000000, XZR, rn, rm) }
func (a *Asm) CmpReg32(rn, rm Reg) { a.addSubReg(0x6B000000, XZR, rn, rm) }

// --- Add/sub (immediate, 0..4095, optional LSL #12) ---

// addSubImm encodes an unshifted 12-bit immediate. imm must be in [0,4095].
func (a *Asm) addSubImm(base uint32, rd, rn Reg, imm uint32) {
	a.word(base | (imm&0xFFF)<<10 | r(rn)<<5 | r(rd))
}

func (a *Asm) AddImm64(rd, rn Reg, imm uint32)  { a.addSubImm(0x91000000, rd, rn, imm) }
func (a *Asm) AddImm32(rd, rn Reg, imm uint32)  { a.addSubImm(0x11000000, rd, rn, imm) }
func (a *Asm) SubImm64(rd, rn Reg, imm uint32)  { a.addSubImm(0xD1000000, rd, rn, imm) }
func (a *Asm) SubImm32(rd, rn Reg, imm uint32)  { a.addSubImm(0x51000000, rd, rn, imm) }
func (a *Asm) SubsImm64(rd, rn Reg, imm uint32) { a.addSubImm(0xF1000000, rd, rn, imm) }

// CmpImm64 is SUBS XZR, Rn, #imm12.
func (a *Asm) CmpImm64(rn Reg, imm uint32) { a.addSubImm(0xF1000000, XZR, rn, imm) }
func (a *Asm) CmpImm32(rn Reg, imm uint32) { a.addSubImm(0x71000000, XZR, rn, imm) }

// SubSP64 / AddSP64 adjust the stack pointer (Rn/Rd = 31 means SP here).
func (a *Asm) SubSP64(imm uint32) { a.addSubImm(0xD1000000, SP, SP, imm) }
func (a *Asm) AddSP64(imm uint32) { a.addSubImm(0x91000000, SP, SP, imm) }

// --- Moves ---

// MovReg64 is ORR Xd, XZR, Xm.
func (a *Asm) MovReg64(rd, rm Reg) { a.word(0xAA000000 | r(rm)<<16 | r(XZR)<<5 | r(rd)) }
func (a *Asm) MovReg32(rd, rm Reg) { a.word(0x2A000000 | r(rm)<<16 | r(XZR)<<5 | r(rd)) }

// movWide encodes MOVZ/MOVK/MOVN with a 16-bit immediate at halfword hw (0..3).
func (a *Asm) movWide(base uint32, rd Reg, imm16 uint16, hw uint32) {
	a.word(base | (hw&3)<<21 | uint32(imm16)<<5 | r(rd))
}

func (a *Asm) Movz64(rd Reg, imm16 uint16, hw uint32) { a.movWide(0xD2800000, rd, imm16, hw) }
func (a *Asm) Movk64(rd Reg, imm16 uint16, hw uint32) { a.movWide(0xF2800000, rd, imm16, hw) }
func (a *Asm) Movn64(rd Reg, imm16 uint16, hw uint32) { a.movWide(0x92800000, rd, imm16, hw) }

// MovImm64 materializes an arbitrary 64-bit constant into rd using the shortest
// MOVZ/MOVN + MOVK sequence (1..4 instructions), mirroring the amd64 encoder's
// MovImm64. Picks MOVN-based when the value has more 0xFFFF halfwords than 0x0000.
func (a *Asm) MovImm64(rd Reg, val uint64) {
	// Count halfwords equal to 0x0000 and 0xFFFF to choose MOVZ vs MOVN start.
	var zeros, ones int
	for i := 0; i < 4; i++ {
		h := uint16(val >> (16 * i))
		if h == 0 {
			zeros++
		} else if h == 0xFFFF {
			ones++
		}
	}
	if ones > zeros {
		// MOVN base: inverse of the first non-0xFFFF halfword, then MOVK the rest.
		first := true
		inv := ^val
		for i := 0; i < 4; i++ {
			h := uint16(inv >> (16 * i))
			vh := uint16(val >> (16 * i))
			if first {
				if h != 0 { // find the first halfword whose inverse is nonzero
					a.Movn64(rd, h, uint32(i))
					first = false
				}
				continue
			}
			if vh != 0xFFFF {
				a.Movk64(rd, vh, uint32(i))
			}
		}
		if first { // val was all-ones
			a.Movn64(rd, 0, 0)
		}
		return
	}
	first := true
	for i := 0; i < 4; i++ {
		h := uint16(val >> (16 * i))
		if first {
			if h != 0 {
				a.Movz64(rd, h, uint32(i))
				first = false
			}
			continue
		}
		if h != 0 {
			a.Movk64(rd, h, uint32(i))
		}
	}
	if first { // val == 0
		a.Movz64(rd, 0, 0)
	}
}

// --- Loads / stores (unsigned scaled immediate offset) ---
// off is a BYTE offset; it must be a multiple of the access size and, divided by
// the size, fit in 12 bits. ok=false otherwise (caller must fall back to a
// register-offset form or address materialization).

func (a *Asm) ldrStr(base uint32, sizeLog uint, rt, rn Reg, off uint32) bool {
	scaled := off >> sizeLog
	if scaled<<sizeLog != off || scaled > 0xFFF {
		return false
	}
	a.word(base | scaled<<10 | r(rn)<<5 | r(rt))
	return true
}

func (a *Asm) Load64(rt, rn Reg, off uint32) bool  { return a.ldrStr(0xF9400000, 3, rt, rn, off) }
func (a *Asm) Store64(rt, rn Reg, off uint32) bool { return a.ldrStr(0xF9000000, 3, rt, rn, off) }
func (a *Asm) Load32(rt, rn Reg, off uint32) bool  { return a.ldrStr(0xB9400000, 2, rt, rn, off) }
func (a *Asm) Store32(rt, rn Reg, off uint32) bool { return a.ldrStr(0xB9000000, 2, rt, rn, off) }
func (a *Asm) Ldrb(rt, rn Reg, off uint32) bool    { return a.ldrStr(0x39400000, 0, rt, rn, off) }
func (a *Asm) Strb(rt, rn Reg, off uint32) bool    { return a.ldrStr(0x39000000, 0, rt, rn, off) }
func (a *Asm) Ldrh(rt, rn Reg, off uint32) bool    { return a.ldrStr(0x79400000, 1, rt, rn, off) }
func (a *Asm) Strh(rt, rn Reg, off uint32) bool    { return a.ldrStr(0x79000000, 1, rt, rn, off) }

// --- Frame pair ops (pre/post-indexed STP/LDP of two X registers) ---
// imm is a BYTE offset, a signed multiple of 8 in [-512, 504].

func (a *Asm) pairImm7(imm int32) uint32 { return uint32((imm/8)&0x7F) << 15 }

// StpPre stores rt,rt2 at [rn, #imm]! (pre-index, writes rn back).
func (a *Asm) StpPre(rt, rt2, rn Reg, imm int32) {
	a.word(0xA9800000 | a.pairImm7(imm) | r(rt2)<<10 | r(rn)<<5 | r(rt))
}

// LdpPost loads rt,rt2 from [rn], #imm (post-index, writes rn back).
func (a *Asm) LdpPost(rt, rt2, rn Reg, imm int32) {
	a.word(0xA8C00000 | a.pairImm7(imm) | r(rt2)<<10 | r(rn)<<5 | r(rt))
}

// --- Bit-count / multiply ---

// Madd64 is Rd = Ra + Rn*Rm. Mul is MADD with Ra = XZR.
func (a *Asm) Madd64(rd, rn, rm, ra Reg) {
	a.word(0x9B000000 | r(rm)<<16 | r(ra)<<10 | r(rn)<<5 | r(rd))
}
func (a *Asm) Mul64(rd, rn, rm Reg) { a.Madd64(rd, rn, rm, XZR) }
func (a *Asm) Mul32(rd, rn, rm Reg) { a.word(0x1B000000 | r(rm)<<16 | r(XZR)<<10 | r(rn)<<5 | r(rd)) }

// --- Logical (shifted register, LSL #0) ---

func (a *Asm) And32(rd, rn, rm Reg) { a.word(0x0A000000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Orr32(rd, rn, rm Reg) { a.word(0x2A000000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Eor32(rd, rn, rm Reg) { a.word(0x4A000000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) And64(rd, rn, rm Reg) { a.word(0x8A000000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Orr64(rd, rn, rm Reg) { a.word(0xAA000000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Eor64(rd, rn, rm Reg) { a.word(0xCA000000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }

// --- Variable shifts (LSLV/LSRV/ASRV: shift Rn by Rm mod width) ---

func (a *Asm) Lslv32(rd, rn, rm Reg) { a.word(0x1AC02000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Lsrv32(rd, rn, rm Reg) { a.word(0x1AC02400 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Asrv32(rd, rn, rm Reg) { a.word(0x1AC02800 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Lslv64(rd, rn, rm Reg) { a.word(0x9AC02000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Lsrv64(rd, rn, rm Reg) { a.word(0x9AC02400 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Asrv64(rd, rn, rm Reg) { a.word(0x9AC02800 | r(rm)<<16 | r(rn)<<5 | r(rd)) }

// --- Conditional select / set ---

// Csel64 is Rd = cond ? Rn : Rm.
func (a *Asm) Csel64(rd, rn, rm Reg, c Cond) {
	a.word(0x9A800000 | r(rm)<<16 | uint32(c)<<12 | r(rn)<<5 | r(rd))
}

// Cset64 is Rd = cond ? 1 : 0, encoded as CSINC Rd, XZR, XZR, invert(cond).
func (a *Asm) Cset64(rd Reg, c Cond) {
	a.word(0x9A800400 | r(XZR)<<16 | uint32(c.Invert())<<12 | r(XZR)<<5 | r(rd))
}
func (a *Asm) Cset32(rd Reg, c Cond) {
	a.word(0x1A800400 | r(XZR)<<16 | uint32(c.Invert())<<12 | r(XZR)<<5 | r(rd))
}

// --- Logical (bitmask immediate) ---

// AndImm64 / OrrImm64 / EorImm64 encode a logical operation with a bitmask
// immediate. Returns false when val is not encodable as an AArch64 bitmask
// immediate (all-zeros, all-ones, or a non-repeating pattern) — the caller must
// then materialize the constant in a register and use the register form.
func (a *Asm) AndImm64(rd, rn Reg, val uint64) bool { return a.logicalImm(0x92000000, rd, rn, val) }
func (a *Asm) OrrImm64(rd, rn Reg, val uint64) bool { return a.logicalImm(0xB2000000, rd, rn, val) }
func (a *Asm) EorImm64(rd, rn Reg, val uint64) bool { return a.logicalImm(0xD2000000, rd, rn, val) }

func (a *Asm) logicalImm(base uint32, rd, rn Reg, val uint64) bool {
	n, immr, imms, ok := encodeLogicalImm(val, true)
	if !ok {
		return false
	}
	a.word(base | n<<22 | immr<<16 | imms<<10 | r(rn)<<5 | r(rd))
	return true
}

// encodeLogicalImm converts a constant to the AArch64 N:immr:imms bitmask-immediate
// fields (the DecodeBitMasks inverse; algorithm from LLVM
// AArch64_AM::processLogicalImmediate). ok=false for all-zeros / all-ones / any
// value that is not a rotated run of ones repeated at a power-of-two element size.
func encodeLogicalImm(imm uint64, is64 bool) (n, immr, imms uint32, ok bool) {
	if !is64 {
		imm &= 0xFFFFFFFF
		imm |= imm << 32 // replicate into the high half so the search is uniform
	}
	if imm == 0 || imm == ^uint64(0) {
		return 0, 0, 0, false
	}
	// Element size: the smallest power-of-two width at which the pattern repeats.
	size := 64
	for size > 2 {
		size /= 2
		mask := (uint64(1) << uint(size)) - 1
		if imm&mask != (imm>>uint(size))&mask {
			size *= 2
			break
		}
	}
	mask := ^uint64(0)
	if size < 64 {
		mask = (uint64(1) << uint(size)) - 1
	}
	e := imm & mask
	ones := bitCount(e)
	if ones == 0 || ones == size {
		return 0, 0, 0, false
	}
	// The element must be a rotation of a contiguous run of `ones` ones.
	run := (uint64(1) << uint(ones)) - 1
	rot := -1
	for k := 0; k < size; k++ {
		cand := ((run << uint(k)) | (run >> uint(size-k))) & mask
		if cand == e {
			rot = k
			break
		}
	}
	if rot < 0 {
		return 0, 0, 0, false
	}
	immr = uint32((size - rot) % size)
	// imms high bits mark the element size (a run of ones for esize<64 that the
	// decoder finds via HighestSetBit(N:NOT(imms))); low log2(esize) bits hold
	// ones-1. N is set only for a 64-bit element.
	if size == 64 {
		n = 1
	}
	imms = (^uint32((size<<1)-1))&0x3F | uint32(ones-1)
	return n, immr, imms, true
}

func bitCount(x uint64) int {
	c := 0
	for x != 0 {
		x &= x - 1
		c++
	}
	return c
}

// --- Branches / calls ---

func (a *Asm) Ret()       { a.word(0xD65F0000 | r(LR)<<5) }
func (a *Asm) Br(rn Reg)  { a.word(0xD61F0000 | r(rn)<<5) }
func (a *Asm) Blr(rn Reg) { a.word(0xD63F0000 | r(rn)<<5) }

// Branch emits an unconditional branch with a zero displacement and returns its
// byte offset; patch it with PatchBranch26 once the target is known.
func (a *Asm) Branch() int { at := a.Len(); a.word(0x14000000); return at }

// Bcond emits a conditional branch with a zero displacement; patch with
// PatchBranch19.
func (a *Asm) Bcond(c Cond) int { at := a.Len(); a.word(0x54000000 | uint32(c)); return at }

// Cbz / Cbnz emit a compare-and-branch on Rt==0 / Rt!=0 with a zero displacement;
// patch with PatchBranch19.
func (a *Asm) Cbz64(rt Reg) int  { at := a.Len(); a.word(0xB4000000 | r(rt)); return at }
func (a *Asm) Cbnz64(rt Reg) int { at := a.Len(); a.word(0xB5000000 | r(rt)); return at }

// PatchBranch26 fills the imm26 field of the B at byte offset `at` to jump to
// byte offset `target`. Returns false if the (word) displacement is out of the
// ±128 MiB range.
func (a *Asm) PatchBranch26(at, target int) bool {
	d := (target - at) / 4
	if d < -(1<<25) || d >= (1<<25) {
		return false
	}
	a.patchWord(at, uint32(d)&0x03FFFFFF)
	return true
}

// PatchBranch19 fills the imm19 field (bits 23:5) of a B.cond / CBZ / CBNZ at
// byte offset `at` to jump to byte offset `target`. Returns false if out of the
// ±1 MiB range.
func (a *Asm) PatchBranch19(at, target int) bool {
	d := (target - at) / 4
	if d < -(1<<18) || d >= (1<<18) {
		return false
	}
	a.patchWord(at, (uint32(d)&0x7FFFF)<<5)
	return true
}
