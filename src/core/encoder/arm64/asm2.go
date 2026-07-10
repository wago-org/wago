package arm64

import "os"

var foldIdxDispEnabled = os.Getenv("WAGO_ARM64_NO_FOLD_IDX_DISP") != "1"

// Port batch: integer data-processing methods the railshot arm64 backend needs
// beyond the base set in asm.go. Base opcode words are verified against clang +
// llvm-objdump goldens in asm2_test.go. By convention here `w bool` selects the
// 32-bit W-form (w == true) vs the 64-bit X-form, matching the port's cheatsheet.

// wbase picks the 32-bit (w) or 64-bit base opcode.
func wbase(w bool, base32, base64 uint32) uint32 {
	if w {
		return base32
	}
	return base64
}

// AddShifted is ADD rd, rn, rm, LSL #shift (shift 0..63/31).
func (a *Asm) AddShifted(rd, rn, rm Reg, shift uint8, w bool) {
	a.word(wbase(w, 0x0B000000, 0x8B000000) | r(rm)<<16 | (uint32(shift)&0x3F)<<10 | r(rn)<<5 | r(rd))
}

// AddExtUXTW is ADD Xd, Xn, Wm, UXTW — the 64-bit extended-register add that
// zero-extends Rm's low 32 bits before adding. It folds `i64.extend_i32_u(y)`
// into an add without a separate zero-extend. option=UXTW(010), imm3=0.
func (a *Asm) AddExtUXTW(rd, rn, rm Reg) {
	a.word(0x8B204000 | r(rm)<<16 | r(rn)<<5 | r(rd))
}

// Adds32 is 32-bit flag-setting ADD (Adds64 is in asm.go).
func (a *Asm) Adds32(rd, rn, rm Reg) { a.word(0x2B000000 | r(rm)<<16 | r(rn)<<5 | r(rd)) }

// Sign-extends (SBFM aliases).
func (a *Asm) Sxtw(rd, rn Reg)         { a.word(0x93407C00 | r(rn)<<5 | r(rd)) }
func (a *Asm) Sxtb(rd, rn Reg, w bool) { a.word(wbase(w, 0x13001C00, 0x93401C00) | r(rn)<<5 | r(rd)) }
func (a *Asm) Sxth(rd, rn Reg, w bool) { a.word(wbase(w, 0x13003C00, 0x93403C00) | r(rn)<<5 | r(rd)) }

// bfm emits a UBFM/SBFM-family instruction with the given immr/imms.
func (a *Asm) bfm(base32, base64 uint32, rd, rn Reg, immr, imms uint32, w bool) {
	a.word(wbase(w, base32, base64) | (immr&0x3F)<<16 | (imms&0x3F)<<10 | r(rn)<<5 | r(rd))
}

// LslImm / LsrImm / AsrImm are the immediate shifts (UBFM/SBFM aliases).
func (a *Asm) LslImm(rd, rn Reg, sh uint8, w bool) {
	width := uint32(64)
	if w {
		width = 32
	}
	s := uint32(sh)
	a.bfm(0x53000000, 0xD3400000, rd, rn, (width-s)&(width-1), width-1-s, w)
}
func (a *Asm) LsrImm(rd, rn Reg, sh uint8, w bool) {
	width := uint32(64)
	if w {
		width = 32
	}
	a.bfm(0x53000000, 0xD3400000, rd, rn, uint32(sh), width-1, w)
}
func (a *Asm) AsrImm(rd, rn Reg, sh uint8, w bool) {
	width := uint32(64)
	if w {
		width = 32
	}
	a.bfm(0x13000000, 0x93400000, rd, rn, uint32(sh), width-1, w)
}
func (a *Asm) LslImm64(rd, rn Reg, sh uint8) { a.LslImm(rd, rn, sh, false) }
func (a *Asm) LsrImm32(rd, rn Reg, sh uint8) { a.LsrImm(rd, rn, sh, true) }
func (a *Asm) AsrImm64(rd, rn Reg, sh uint8) { a.AsrImm(rd, rn, sh, false) }

// RorImm is ROR rd, rn, #sh, encoded as EXTR rd, rn, rn, #sh.
func (a *Asm) RorImm(rd, rn Reg, sh uint8, w bool) {
	a.word(wbase(w, 0x13800000, 0x93C00000) | r(rn)<<16 | (uint32(sh)&0x3F)<<10 | r(rn)<<5 | r(rd))
}

// Variable rotate right.
func (a *Asm) Rorv32(rd, rn, rm Reg) { a.word(0x1AC02C00 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Rorv64(rd, rn, rm Reg) { a.word(0x9AC02C00 | r(rm)<<16 | r(rn)<<5 | r(rd)) }

// Clz / Rbit (ctz is RBIT then CLZ).
func (a *Asm) Clz(rd, rn Reg, w bool)  { a.word(wbase(w, 0x5AC01000, 0xDAC01000) | r(rn)<<5 | r(rd)) }
func (a *Asm) Rbit(rd, rn Reg, w bool) { a.word(wbase(w, 0x5AC00000, 0xDAC00000) | r(rn)<<5 | r(rd)) }

// Divide + multiply-subtract (remainder = Rn - (Rn/Rm)*Rm via div then Msub).
func (a *Asm) Sdiv32(rd, rn, rm Reg) { a.word(0x1AC00C00 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Sdiv64(rd, rn, rm Reg) { a.word(0x9AC00C00 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Udiv32(rd, rn, rm Reg) { a.word(0x1AC00800 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Udiv64(rd, rn, rm Reg) { a.word(0x9AC00800 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Msub32(rd, rn, rm, ra Reg) {
	a.word(0x1B008000 | r(rm)<<16 | r(ra)<<10 | r(rn)<<5 | r(rd))
}
func (a *Asm) Msub64(rd, rn, rm, ra Reg) {
	a.word(0x9B008000 | r(rm)<<16 | r(ra)<<10 | r(rn)<<5 | r(rd))
}

// CMN (ADDS to XZR) with immediate — compare against a negative value.
func (a *Asm) CmnImm32(rn Reg, imm uint32) { a.word(0x3100001F | (imm&0xFFF)<<10 | r(rn)<<5) }
func (a *Asm) CmnImm64(rn Reg, imm uint32) { a.word(0xB100001F | (imm&0xFFF)<<10 | r(rn)<<5) }

// High/long multiplies for magic-number division.
func (a *Asm) Smulh(rd, rn, rm Reg) { a.word(0x9B407C00 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Umulh(rd, rn, rm Reg) { a.word(0x9BC07C00 | r(rm)<<16 | r(rn)<<5 | r(rd)) }
func (a *Asm) Smull(rd, rn, rm Reg) { a.word(0x9B207C00 | r(rm)<<16 | r(rn)<<5 | r(rd)) } // Xd, Wn, Wm
func (a *Asm) Umull(rd, rn, rm Reg) { a.word(0x9BA07C00 | r(rm)<<16 | r(rn)<<5 | r(rd)) }

// Csel32 is the 32-bit conditional select (Csel64 is in asm.go).
func (a *Asm) Csel32(rd, rn, rm Reg, c Cond) {
	a.word(0x1A800000 | r(rm)<<16 | uint32(c)<<12 | r(rn)<<5 | r(rd))
}

// 32-bit logical immediates (return false when val is not an encodable bitmask).
func (a *Asm) AndImm32(rd, rn Reg, val uint32) bool { return a.logicalImm32(0x12000000, rd, rn, val) }
func (a *Asm) OrrImm32(rd, rn Reg, val uint32) bool { return a.logicalImm32(0x32000000, rd, rn, val) }
func (a *Asm) EorImm32(rd, rn Reg, val uint32) bool { return a.logicalImm32(0x52000000, rd, rn, val) }

func (a *Asm) logicalImm32(base uint32, rd, rn Reg, val uint32) bool {
	n, immr, imms, ok := encodeLogicalImm(uint64(val), false)
	if !ok || n != 0 { // the 32-bit form has no N bit
		return false
	}
	a.word(base | immr<<16 | imms<<10 | r(rn)<<5 | r(rd))
	return true
}

// --- Scalar floating-point (S = single, D = double; V-register numbering) ---

func fbase(f64 bool, baseS, baseD uint32) uint32 {
	if f64 {
		return baseD
	}
	return baseS
}

func (a *Asm) Fadd(rd, rn, rm Reg, f64 bool) {
	a.word(fbase(f64, 0x1E202800, 0x1E602800) | r(rm)<<16 | r(rn)<<5 | r(rd))
}
func (a *Asm) Fsub(rd, rn, rm Reg, f64 bool) {
	a.word(fbase(f64, 0x1E203800, 0x1E603800) | r(rm)<<16 | r(rn)<<5 | r(rd))
}
func (a *Asm) Fmul(rd, rn, rm Reg, f64 bool) {
	a.word(fbase(f64, 0x1E200800, 0x1E600800) | r(rm)<<16 | r(rn)<<5 | r(rd))
}
func (a *Asm) Fdiv(rd, rn, rm Reg, f64 bool) {
	a.word(fbase(f64, 0x1E201800, 0x1E601800) | r(rm)<<16 | r(rn)<<5 | r(rd))
}
func (a *Asm) Fsqrt(rd, rn Reg, f64 bool) {
	a.word(fbase(f64, 0x1E21C000, 0x1E61C000) | r(rn)<<5 | r(rd))
}
func (a *Asm) Fmin(rd, rn, rm Reg, f64 bool) {
	a.word(fbase(f64, 0x1E205800, 0x1E605800) | r(rm)<<16 | r(rn)<<5 | r(rd))
}
func (a *Asm) Fmax(rd, rn, rm Reg, f64 bool) {
	a.word(fbase(f64, 0x1E204800, 0x1E604800) | r(rm)<<16 | r(rn)<<5 | r(rd))
}

// FmovReg copies V→V; FmovFromGpr copies GPR→V (also +0.0 from XZR/WZR);
// FmovToGpr copies V→GPR (reinterpret bits).
func (a *Asm) FmovReg(rd, rn Reg, f64 bool) {
	a.word(fbase(f64, 0x1E204000, 0x1E604000) | r(rn)<<5 | r(rd))
}
func (a *Asm) FmovFromGpr(rd, rn Reg, f64 bool) {
	a.word(fbase(f64, 0x1E270000, 0x9E670000) | r(rn)<<5 | r(rd))
}
func (a *Asm) FmovToGpr(rd, rn Reg, f64 bool) {
	a.word(fbase(f64, 0x1E260000, 0x9E660000) | r(rn)<<5 | r(rd))
}

// Fcmp sets NZCV from Rn ? Rm.
func (a *Asm) Fcmp(rn, rm Reg, f64 bool) {
	a.word(fbase(f64, 0x1E202000, 0x1E602000) | r(rm)<<16 | r(rn)<<5)
}

// Frint rounds to integral: mode 'n' (nearest), 'm' (floor), 'p' (ceil), 'z' (trunc).
func (a *Asm) Frint(rd, rn Reg, f64 bool, mode byte) {
	var s, d uint32
	switch mode {
	case 'n':
		s, d = 0x1E244000, 0x1E644000
	case 'm':
		s, d = 0x1E254000, 0x1E654000
	case 'p':
		s, d = 0x1E24C000, 0x1E64C000
	case 'z':
		s, d = 0x1E25C000, 0x1E65C000
	default:
		panic("Frint: bad mode")
	}
	a.word(fbase(f64, s, d) | r(rn)<<5 | r(rd))
}

// Fcvtzs converts float→signed int (round toward zero). f64src selects the source
// precision, dstWide selects an X (64-bit) vs W (32-bit) destination.
func (a *Asm) Fcvtzs(rd, rn Reg, f64src, dstWide bool) {
	base := uint32(0x1E380000)
	if dstWide {
		base |= 0x80000000
	}
	if f64src {
		base |= 0x00400000
	}
	a.word(base | r(rn)<<5 | r(rd))
}

// Scvtf converts signed int→float. f64 selects dest precision, srcWide selects an
// X (64-bit) vs W (32-bit) source.
func (a *Asm) Scvtf(rd, rn Reg, f64, srcWide bool) {
	base := uint32(0x1E220000)
	if f64 {
		base |= 0x00400000
	}
	if srcWide {
		base |= 0x80000000
	}
	a.word(base | r(rn)<<5 | r(rd))
}

// Ucvtf converts unsigned int→float. f64 selects destination precision and
// srcWide selects an X (64-bit) rather than W (32-bit) source.
func (a *Asm) Ucvtf(rd, rn Reg, f64, srcWide bool) {
	base := uint32(0x1E230000)
	if f64 {
		base |= 0x00400000
	}
	if srcWide {
		base |= 0x80000000
	}
	a.word(base | r(rn)<<5 | r(rd))
}
func (a *Asm) CvtI2F(rd, rn Reg, f64, srcWide bool) { a.Scvtf(rd, rn, f64, srcWide) }

func (a *Asm) FcvtS2D(rd, rn Reg) { a.word(0x1E22C000 | r(rn)<<5 | r(rd)) } // promote single→double
func (a *Asm) FcvtD2S(rd, rn Reg) { a.word(0x1E624000 | r(rn)<<5 | r(rd)) } // demote double→single

// --- Stack-pointer register forms (SP must use the extended-register encoding) ---

func (a *Asm) SubSPReg(rm Reg) { a.word(0xCB2063FF | r(rm)<<16) } // SUB SP, SP, Xm
func (a *Asm) AddSPReg(rm Reg) { a.word(0x8B2063FF | r(rm)<<16) } // ADD SP, SP, Xm
func (a *Asm) CmpSP64(rm Reg)  { a.word(0xEB2063FF | r(rm)<<16) } // CMP SP, Xm (stack fence)

// --- Branch-with-link, ADR, and patch helpers ---

// Bl emits BL with a zero displacement, returning its byte offset (patch with
// PatchBranch26 at module layout — same imm26 field as B).
func (a *Asm) Bl() int { at := a.Len(); a.word(0x94000000); return at }

// Adr emits ADR rd with a zero PC-relative displacement, returning its byte
// offset (patch with PatchAdr). Range ±1 MiB.
func (a *Asm) Adr(rd Reg) int { at := a.Len(); a.word(0x10000000 | r(rd)); return at }

// PatchAdr fills the split immlo:immhi (21-bit signed) of an ADR at byte offset
// `at` to address byte offset `target`. Returns false if out of ±1 MiB range.
func (a *Asm) PatchAdr(at, target int) bool {
	d := target - at
	if d < -(1<<20) || d >= (1<<20) {
		return false
	}
	imm := uint32(d) & 0x1FFFFF
	a.patchWord(at, (imm&3)<<29|((imm>>2)&0x7FFFF)<<5)
	return true
}

// PatchU32 overwrites the raw 32-bit little-endian word at byte offset `at` (used
// for br_table jump-table data entries, which are data, not instructions).
func (a *Asm) PatchU32(at int, val uint32) {
	a.B[at] = byte(val)
	a.B[at+1] = byte(val >> 8)
	a.B[at+2] = byte(val >> 16)
	a.B[at+3] = byte(val >> 24)
}

// PatchMovImm rewrites the imm16 fields of a reserved MOVZ/MOVK pair (at `at` and
// `at`+4) with the low and high halfwords of val — used to backpatch the frame
// size into a prologue placeholder.
func (a *Asm) PatchMovImm(at int, val uint32) {
	set := func(pos int, imm16 uint32) {
		w := a.wordAt(pos)&^(0xFFFF<<5) | (imm16&0xFFFF)<<5
		a.B[pos] = byte(w)
		a.B[pos+1] = byte(w >> 8)
		a.B[pos+2] = byte(w >> 16)
		a.B[pos+3] = byte(w >> 24)
	}
	set(at, val&0xFFFF)
	set(at+4, (val>>16)&0xFFFF)
}

// --- Loads / stores: register-offset (byte offset, unscaled — for linMem) ---

func sizeField(size int) uint32 {
	switch size {
	case 2:
		return 1 << 30
	case 4:
		return 2 << 30
	case 8:
		return 3 << 30
	}
	return 0 // size 1
}

// LdrIdx loads rt = [rn + rm] (rm is a byte offset, no scaling). size is 1/2/4/8;
// signed sign-extends a sub-word load, with wideDest selecting an X (64-bit) vs W
// destination for the sign-extended value.
func (a *Asm) LdrIdx(rt, rn, rm Reg, size int, signed, wideDest bool) {
	opc := uint32(1) // load, zero-extend
	if signed {
		opc = 3 // sign-extend to 32
		if wideDest {
			opc = 2 // sign-extend to 64
		}
	}
	a.word(0x38206800 | sizeField(size) | opc<<22 | r(rm)<<16 | r(rn)<<5 | r(rt))
}

// StrIdx stores the low `size` bytes of rt to [rn + rm] (unscaled byte offset).
func (a *Asm) StrIdx(rt, rn, rm Reg, size int) {
	a.word(0x38206800 | sizeField(size) | r(rm)<<16 | r(rn)<<5 | r(rt))
}

// --- Loads / stores: scalar-FP + vector scaled-immediate offset ---

func (a *Asm) ldStrScaled(base uint32, shift uint, rt, rn Reg, off uint32) bool {
	s := off >> shift
	if s<<shift != off || s > 0xFFF {
		return false
	}
	a.word(base | s<<10 | r(rn)<<5 | r(rt))
	return true
}

func (a *Asm) LdrS(dst, base Reg, disp int32) {
	if !a.ldStrScaled(0xBD400000, 2, dst, base, uint32(disp)) {
		panic("LdrS: bad offset")
	}
}
func (a *Asm) LdrD(dst, base Reg, disp int32) {
	if !a.ldStrScaled(0xFD400000, 3, dst, base, uint32(disp)) {
		panic("LdrD: bad offset")
	}
}
func (a *Asm) StrS(base Reg, disp int32, src Reg) {
	if !a.ldStrScaled(0xBD000000, 2, src, base, uint32(disp)) {
		panic("StrS: bad offset")
	}
}
func (a *Asm) StrD(base Reg, disp int32, src Reg) {
	if !a.ldStrScaled(0xFD000000, 3, src, base, uint32(disp)) {
		panic("StrD: bad offset")
	}
}

// LdrQ / StrQ are 128-bit spill load/store with a signed byte displacement,
// matching the backend's amd64-legacy call shape (dst,base,disp)/(base,disp,src).
func (a *Asm) LdrQ(dst, base Reg, disp int32) {
	if a.ldStrScaled(0x3DC00000, 4, dst, base, uint32(disp)) {
		return
	}
	a.AddImm64(X16, base, uint32(disp))
	a.ldStrScaled(0x3DC00000, 4, dst, X16, 0)
}
func (a *Asm) StrQ(base Reg, disp int32, src Reg) {
	if a.ldStrScaled(0x3D800000, 4, src, base, uint32(disp)) {
		return
	}
	a.AddImm64(X16, base, uint32(disp))
	a.ldStrScaled(0x3D800000, 4, src, X16, 0)
}

// LoadIdx / StoreIdx / StoreImmIdx are the base+index(+disp) linear-memory
// accessors the port calls with the amd64 shape. AArch64 has no base+index+disp
// form, so a nonzero displacement is folded by computing the effective address
// `base + index + disp` into the reserved scratch X16 (IP0, never an operand
// register per CONTRACT §2), then doing a plain [X16] access.

// eaX16 leaves base + index + disp in X16 (IP0). Uses X17 only for a wide disp.
func (a *Asm) eaX16(base, index Reg, disp int32) {
	a.AddShifted(X16, base, index, 0, false) // X16 = base + index
	a.addDispX16(disp)
}

func (a *Asm) addDispX16(disp int32) {
	switch {
	case disp == 0:
	case disp > 0 && disp <= 0xFFF:
		a.AddImm64(X16, X16, uint32(disp))
	case disp < 0 && -disp <= 0xFFF:
		a.SubImm64(X16, X16, uint32(-disp))
	default:
		a.MovImm64(X17, uint64(int64(disp)))
		a.AddShifted(X16, X16, X17, 0, false)
	}
}

func (a *Asm) loadDisp(dst, base Reg, disp int32, size int, signed, wideDest bool) bool {
	if disp < 0 {
		return false
	}
	off := uint32(disp)
	if !signed {
		switch size {
		case 1:
			return a.Ldrb(dst, base, off)
		case 2:
			return a.Ldrh(dst, base, off)
		case 4:
			return a.Load32(dst, base, off)
		case 8:
			return a.Load64(dst, base, off)
		}
	}
	var op uint32
	switch size {
	case 1:
		if wideDest {
			op = 0x39800000 // LDRSB Xt
		} else {
			op = 0x39C00000 // LDRSB Wt
		}
	case 2:
		if wideDest {
			op = 0x79800000 // LDRSH Xt
		} else {
			op = 0x79C00000 // LDRSH Wt
		}
	case 4:
		if wideDest {
			op = 0xB9800000 // LDRSW Xt
		} else {
			return a.Load32(dst, base, off)
		}
	default:
		return false
	}
	shift := uint(0)
	for 1<<shift < size {
		shift++
	}
	return a.ldStrScaled(op, shift, dst, base, off)
}

func (a *Asm) storeDisp(src, base Reg, disp int32, size int) bool {
	if disp < 0 {
		return false
	}
	off := uint32(disp)
	switch size {
	case 1:
		return a.Strb(src, base, off)
	case 2:
		return a.Strh(src, base, off)
	case 4:
		return a.Store32(src, base, off)
	case 8:
		return a.Store64(src, base, off)
	}
	return false
}

func (a *Asm) LoadIdx(dst, base, index Reg, disp int32, size int, signed, wideDest bool) {
	if disp == 0 {
		a.LdrIdx(dst, base, index, size, signed, wideDest)
		return
	}
	if foldIdxDispEnabled && a.DenseIdxDisp {
		a.AddShifted(X16, base, index, 0, false)
		if a.loadDisp(dst, X16, disp, size, signed, wideDest) {
			return
		}
	} else {
		a.AddShifted(X16, base, index, 0, false)
	}
	a.addDispX16(disp)
	a.LdrIdx(dst, X16, XZR, size, signed, wideDest)
}
func (a *Asm) StoreIdx(base, index, src Reg, disp int32, size int) {
	if disp == 0 {
		a.StrIdx(src, base, index, size)
		return
	}
	if foldIdxDispEnabled && a.DenseIdxDisp {
		a.AddShifted(X16, base, index, 0, false)
		if a.storeDisp(src, X16, disp, size) {
			return
		}
	} else {
		a.AddShifted(X16, base, index, 0, false)
	}
	a.addDispX16(disp)
	a.StrIdx(src, X16, XZR, size)
}
func (a *Asm) StoreImmIdx(base, index Reg, disp, val int32, size int) {
	src := Reg(XZR)
	if val != 0 {
		a.MovImm64(X17, uint64(uint32(val)))
		src = X17
	}
	if disp == 0 {
		a.StrIdx(src, base, index, size)
		return
	}
	if foldIdxDispEnabled && a.DenseIdxDisp {
		a.AddShifted(X16, base, index, 0, false)
		if a.storeDisp(src, X16, disp, size) {
			return
		}
	} else {
		a.AddShifted(X16, base, index, 0, false)
	}
	a.addDispX16(disp)
	a.StrIdx(src, X16, XZR, size)
}

// FMov is the amd64-legacy alias for a scalar V→V move.
func (a *Asm) FMov(rd, rn Reg, f64 bool) { a.FmovReg(rd, rn, f64) }

// --- Loads / stores: scalar-FP + vector register-offset (unscaled) ---

func (a *Asm) LdrFIdx(dst, base, index Reg, disp int32, f64 bool) {
	if disp == 0 {
		a.word(fbase(f64, 0xBC606800, 0xFC606800) | r(index)<<16 | r(base)<<5 | r(dst))
		return
	}
	a.AddShifted(X16, base, index, 0, false)
	if foldIdxDispEnabled && a.DenseIdxDisp {
		shift := uint(2)
		if f64 {
			shift = 3
		}
		if disp >= 0 && a.ldStrScaled(fbase(f64, 0xBD400000, 0xFD400000), shift, dst, X16, uint32(disp)) {
			return
		}
	}
	a.addDispX16(disp)
	a.word(fbase(f64, 0xBC606800, 0xFC606800) | r(XZR)<<16 | r(X16)<<5 | r(dst))
}
func (a *Asm) StrFIdx(base, index, src Reg, disp int32, f64 bool) {
	if disp == 0 {
		a.word(fbase(f64, 0xBC206800, 0xFC206800) | r(index)<<16 | r(base)<<5 | r(src))
		return
	}
	a.AddShifted(X16, base, index, 0, false)
	if foldIdxDispEnabled && a.DenseIdxDisp {
		shift := uint(2)
		if f64 {
			shift = 3
		}
		if disp >= 0 && a.ldStrScaled(fbase(f64, 0xBD000000, 0xFD000000), shift, src, X16, uint32(disp)) {
			return
		}
	}
	a.addDispX16(disp)
	a.word(fbase(f64, 0xBC206800, 0xFC206800) | r(XZR)<<16 | r(X16)<<5 | r(src))
}
func (a *Asm) LdrQIdx(rt, rn, rm Reg, disp int32) {
	if disp == 0 {
		a.word(0x3CE06800 | r(rm)<<16 | r(rn)<<5 | r(rt))
		return
	}
	a.AddShifted(X16, rn, rm, 0, false)
	if foldIdxDispEnabled && a.DenseIdxDisp && disp >= 0 && a.ldStrScaled(0x3DC00000, 4, rt, X16, uint32(disp)) {
		return
	}
	a.addDispX16(disp)
	a.word(0x3CE06800 | r(XZR)<<16 | r(X16)<<5 | r(rt))
}
func (a *Asm) StrQIdx(rn, rm, rt Reg, disp int32) {
	if disp == 0 {
		a.word(0x3CA06800 | r(rm)<<16 | r(rn)<<5 | r(rt))
		return
	}
	a.AddShifted(X16, rn, rm, 0, false)
	if foldIdxDispEnabled && a.DenseIdxDisp && disp >= 0 && a.ldStrScaled(0x3D800000, 4, rt, X16, uint32(disp)) {
		return
	}
	a.addDispX16(disp)
	a.word(0x3CA06800 | r(XZR)<<16 | r(X16)<<5 | r(rt))
}

// LeaSP computes rd = SP + off (off <= 4095), the SP-relative address form.
func (a *Asm) LeaSP(rd Reg, off int32) {
	a.word(0x91000000 | (uint32(off)&0xFFF)<<10 | 31<<5 | r(rd))
}

// Grow reserves room for n more bytes without changing the emitted length.
func (a *Asm) Grow(n int) {
	if n <= 0 || cap(a.B)-len(a.B) >= n {
		return
	}
	b := make([]byte, len(a.B), len(a.B)+n)
	copy(b, a.B)
	a.B = b
}

// FLoadDisp / FStoreDisp are amd64-legacy names for scalar-FP spill load/store;
// on arm64 they are LDR/STR St/Dt with a scaled-imm offset.
func (a *Asm) FLoadDisp(rt, base Reg, disp int32, f64 bool) {
	if f64 {
		a.LdrD(rt, base, disp)
	} else {
		a.LdrS(rt, base, disp)
	}
}
func (a *Asm) FStoreDisp(base Reg, disp int32, rt Reg, f64 bool) {
	if f64 {
		a.StrD(base, disp, rt)
	} else {
		a.StrS(base, disp, rt)
	}
}

// --- Unscaled signed-offset loads/stores (LDUR/STUR, off in -256..255) ---

func (a *Asm) Ldur64(rt, rn Reg, off int32) {
	a.word(0xF8400000 | (uint32(off)&0x1FF)<<12 | r(rn)<<5 | r(rt))
}
func (a *Asm) Ldur32(rt, rn Reg, off int32) {
	a.word(0xB8400000 | (uint32(off)&0x1FF)<<12 | r(rn)<<5 | r(rt))
}
func (a *Asm) Stur64(rt, rn Reg, off int32) {
	a.word(0xF8000000 | (uint32(off)&0x1FF)<<12 | r(rn)<<5 | r(rt))
}
func (a *Asm) Stur32(rt, rn Reg, off int32) {
	a.word(0xB8000000 | (uint32(off)&0x1FF)<<12 | r(rn)<<5 | r(rt))
}

// Nop is the canonical A64 no-op.
func (a *Asm) Nop() { a.word(0xD503201F) }

// Align16 pads the buffer to a 16-byte boundary with NOPs (A64 insns are 4 bytes,
// so the buffer length is always a multiple of 4).
func (a *Asm) Align16() {
	for len(a.B)%16 != 0 {
		a.Nop()
	}
}

// VMovdquLoadDisp / VMovdquStoreDisp are amd64-legacy names the port kept for
// 128-bit spill/restore; on arm64 they are LDR/STR Qt with a scaled-imm offset.
func (a *Asm) VMovdquLoadDisp(dst, base Reg, disp int32)      { a.LdrQ(dst, base, disp) }
func (a *Asm) VMovdquStoreDisp(base Reg, disp int32, src Reg) { a.StrQ(base, disp, src) }

// Csel is the width-parameterized conditional select (w == true → 32-bit).
func (a *Asm) Csel(rd, rn, rm Reg, c Cond, w bool) {
	if w {
		a.Csel32(rd, rn, rm, c)
	} else {
		a.Csel64(rd, rn, rm, c)
	}
}

// NeonMov16b copies a full 128-bit vector: MOV Vd.16b, Vn.16b (ORR Vd,Vn,Vn).
func (a *Asm) NeonMov16b(dst, src Reg) {
	a.word(0x4EA01C00 | r(src)<<16 | r(src)<<5 | r(dst))
}

// Cnt8b / Addv8b are the scalar popcnt reduction pieces. The same CNT encoding
// is also the full-vector i8x16.popcnt lowering.
func (a *Asm) Cnt8b(dst, src Reg)       { a.word(0x4E205800 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonCntB(dst, src Reg)    { a.Cnt8b(dst, src) }
func (a *Asm) Addv8b(dst, src Reg)      { a.word(0x0E31B800 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonUmaxvB(dst, src Reg)  { a.word(0x6E30A800 | r(src)<<5 | r(dst)) }

// Horizontal unsigned-min (UMINV) and full-width add (ADDV) reductions across a
// 128-bit vector. UMINV feeds all_true (a lane is zero iff the min lane is
// zero); ADDV feeds bitmask, where every lane has been ANDed to a distinct
// power-of-two weight so the horizontal sum is exactly the sign-bit mask.
func (a *Asm) NeonUminvB(dst, src Reg) { a.word(0x6E31A800 | r(src)<<5 | r(dst)) } // UMINV b, Vn.16b
func (a *Asm) NeonUminvH(dst, src Reg) { a.word(0x6E71A800 | r(src)<<5 | r(dst)) } // UMINV h, Vn.8h
func (a *Asm) NeonUminvS(dst, src Reg) { a.word(0x6EB1A800 | r(src)<<5 | r(dst)) } // UMINV s, Vn.4s
func (a *Asm) NeonAddvH(dst, src Reg)  { a.word(0x4E71B800 | r(src)<<5 | r(dst)) } // ADDV h, Vn.8h
func (a *Asm) NeonAddvS(dst, src Reg)  { a.word(0x4EB1B800 | r(src)<<5 | r(dst)) } // ADDV s, Vn.4s
func (a *Asm) NeonBsl16b(dst, n, m Reg) { a.word(0x6E601C00 | r(m)<<16 | r(n)<<5 | r(dst)) }

// --- NEON 16-byte logical ops (float sign-bit manipulation) + float spill aliases ---

func (a *Asm) And16b(dst, n, m Reg)     { a.word(0x4E201C00 | r(m)<<16 | r(n)<<5 | r(dst)) }
func (a *Asm) Orr16b(dst, n, m Reg)     { a.word(0x4EA01C00 | r(m)<<16 | r(n)<<5 | r(dst)) }
func (a *Asm) Eor16b(dst, n, m Reg)     { a.word(0x6E201C00 | r(m)<<16 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonAnd16b(dst, n, m Reg) { a.And16b(dst, n, m) }
func (a *Asm) NeonOrr16b(dst, n, m Reg) { a.Orr16b(dst, n, m) }
func (a *Asm) NeonEor16b(dst, n, m Reg) { a.Eor16b(dst, n, m) }
func (a *Asm) NeonNot16b(dst, n Reg)    { a.word(0x6E205800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonAndn16b(dst, n, m Reg) {
	a.word(0x4E601C00 | r(m)<<16 | r(n)<<5 | r(dst)) // BIC Vd.16b,Vn.16b,Vm.16b
}

func neonSize(bytes int) uint32 {
	switch bytes {
	case 1:
		return 0
	case 2:
		return 1
	case 4:
		return 2
	case 8:
		return 3
	default:
		panic("arm64: bad NEON lane size")
	}
}

func (a *Asm) neon3(base uint32, bytes int, dst, n, m Reg) {
	a.word(base | neonSize(bytes)<<22 | r(m)<<16 | r(n)<<5 | r(dst))
}

func (a *Asm) NeonAddB(dst, n, m Reg)  { a.neon3(0x4E208400, 1, dst, n, m) }
func (a *Asm) NeonAddH(dst, n, m Reg)  { a.neon3(0x4E208400, 2, dst, n, m) }
func (a *Asm) NeonAddS(dst, n, m Reg)  { a.neon3(0x4E208400, 4, dst, n, m) }
func (a *Asm) NeonAddpS(dst, n, m Reg) { a.neon3(0x4E20BC00, 4, dst, n, m) }
func (a *Asm) NeonAddD(dst, n, m Reg)  { a.neon3(0x4E208400, 8, dst, n, m) }
func (a *Asm) NeonSubB(dst, n, m Reg)  { a.neon3(0x6E208400, 1, dst, n, m) }
func (a *Asm) NeonSubH(dst, n, m Reg)  { a.neon3(0x6E208400, 2, dst, n, m) }
func (a *Asm) NeonSubS(dst, n, m Reg)  { a.neon3(0x6E208400, 4, dst, n, m) }
func (a *Asm) NeonSubD(dst, n, m Reg)  { a.neon3(0x6E208400, 8, dst, n, m) }

func (a *Asm) NeonSqaddB(dst, n, m Reg) { a.neon3(0x4E200C00, 1, dst, n, m) }
func (a *Asm) NeonSqaddH(dst, n, m Reg) { a.neon3(0x4E200C00, 2, dst, n, m) }
func (a *Asm) NeonUqaddB(dst, n, m Reg) { a.neon3(0x6E200C00, 1, dst, n, m) }
func (a *Asm) NeonUqaddH(dst, n, m Reg) { a.neon3(0x6E200C00, 2, dst, n, m) }
func (a *Asm) NeonSqsubB(dst, n, m Reg) { a.neon3(0x4E202C00, 1, dst, n, m) }
func (a *Asm) NeonSqsubH(dst, n, m Reg) { a.neon3(0x4E202C00, 2, dst, n, m) }
func (a *Asm) NeonUqsubB(dst, n, m Reg) { a.neon3(0x6E202C00, 1, dst, n, m) }
func (a *Asm) NeonUqsubH(dst, n, m Reg) { a.neon3(0x6E202C00, 2, dst, n, m) }

func (a *Asm) NeonCmeqB(dst, n, m Reg) { a.neon3(0x6E208C00, 1, dst, n, m) }
func (a *Asm) NeonCmeqH(dst, n, m Reg) { a.neon3(0x6E208C00, 2, dst, n, m) }
func (a *Asm) NeonCmeqS(dst, n, m Reg) { a.neon3(0x6E208C00, 4, dst, n, m) }
func (a *Asm) NeonCmeqD(dst, n, m Reg) { a.neon3(0x6E208C00, 8, dst, n, m) }
func (a *Asm) NeonCmgtB(dst, n, m Reg) { a.neon3(0x4E203400, 1, dst, n, m) }
func (a *Asm) NeonCmgtH(dst, n, m Reg) { a.neon3(0x4E203400, 2, dst, n, m) }
func (a *Asm) NeonCmgtS(dst, n, m Reg) { a.neon3(0x4E203400, 4, dst, n, m) }
func (a *Asm) NeonCmgtD(dst, n, m Reg) { a.neon3(0x4E203400, 8, dst, n, m) }
func (a *Asm) NeonCmgeB(dst, n, m Reg) { a.neon3(0x4E203C00, 1, dst, n, m) }
func (a *Asm) NeonCmgeH(dst, n, m Reg) { a.neon3(0x4E203C00, 2, dst, n, m) }
func (a *Asm) NeonCmgeS(dst, n, m Reg) { a.neon3(0x4E203C00, 4, dst, n, m) }
func (a *Asm) NeonCmgeD(dst, n, m Reg) { a.neon3(0x4E203C00, 8, dst, n, m) }
func (a *Asm) NeonCmhiB(dst, n, m Reg) { a.neon3(0x6E203400, 1, dst, n, m) }
func (a *Asm) NeonCmhiH(dst, n, m Reg) { a.neon3(0x6E203400, 2, dst, n, m) }
func (a *Asm) NeonCmhiS(dst, n, m Reg) { a.neon3(0x6E203400, 4, dst, n, m) }
func (a *Asm) NeonCmhsB(dst, n, m Reg) { a.neon3(0x6E203C00, 1, dst, n, m) }
func (a *Asm) NeonCmhsH(dst, n, m Reg) { a.neon3(0x6E203C00, 2, dst, n, m) }
func (a *Asm) NeonCmhsS(dst, n, m Reg) { a.neon3(0x6E203C00, 4, dst, n, m) }

func (a *Asm) NeonSminB(dst, n, m Reg) { a.neon3(0x4E206C00, 1, dst, n, m) }
func (a *Asm) NeonSminH(dst, n, m Reg) { a.neon3(0x4E206C00, 2, dst, n, m) }
func (a *Asm) NeonSminS(dst, n, m Reg) { a.neon3(0x4E206C00, 4, dst, n, m) }
func (a *Asm) NeonUminB(dst, n, m Reg) { a.neon3(0x6E206C00, 1, dst, n, m) }
func (a *Asm) NeonUminH(dst, n, m Reg) { a.neon3(0x6E206C00, 2, dst, n, m) }
func (a *Asm) NeonUminS(dst, n, m Reg) { a.neon3(0x6E206C00, 4, dst, n, m) }
func (a *Asm) NeonSmaxB(dst, n, m Reg) { a.neon3(0x4E206400, 1, dst, n, m) }
func (a *Asm) NeonSmaxH(dst, n, m Reg) { a.neon3(0x4E206400, 2, dst, n, m) }
func (a *Asm) NeonSmaxS(dst, n, m Reg) { a.neon3(0x4E206400, 4, dst, n, m) }
func (a *Asm) NeonUmaxB(dst, n, m Reg) { a.neon3(0x6E206400, 1, dst, n, m) }
func (a *Asm) NeonUmaxH(dst, n, m Reg) { a.neon3(0x6E206400, 2, dst, n, m) }
func (a *Asm) NeonUmaxS(dst, n, m Reg) { a.neon3(0x6E206400, 4, dst, n, m) }

func (a *Asm) NeonUrhaddB(dst, n, m Reg)   { a.neon3(0x6E201400, 1, dst, n, m) }
func (a *Asm) NeonUrhaddH(dst, n, m Reg)   { a.neon3(0x6E201400, 2, dst, n, m) }
func (a *Asm) NeonMulH(dst, n, m Reg)      { a.neon3(0x4E209C00, 2, dst, n, m) }
func (a *Asm) NeonMulS(dst, n, m Reg)      { a.neon3(0x4E209C00, 4, dst, n, m) }
func (a *Asm) NeonSqrdmulhH(dst, n, m Reg) { a.neon3(0x6E20B400, 2, dst, n, m) }
func (a *Asm) NeonHaddH(dst, n, m Reg)     { a.neon3(0x4E202800, 2, dst, n, m) }
func (a *Asm) NeonHaddS(dst, n, m Reg)     { a.neon3(0x4E202800, 4, dst, n, m) }
func (a *Asm) NeonSaddlpHfromB(dst, n Reg) { a.word(0x4E202800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUaddlpHfromB(dst, n Reg) { a.word(0x6E202800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSaddlpSfromH(dst, n Reg) { a.word(0x4E602800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUaddlpSfromH(dst, n Reg) { a.word(0x6E602800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSxtlHfromB(dst, n Reg)   { a.word(0x0F08A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSxtl2HfromB(dst, n Reg)  { a.word(0x4F08A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUxtlHfromB(dst, n Reg)   { a.word(0x2F08A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUxtl2HfromB(dst, n Reg)  { a.word(0x6F08A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSxtlSfromH(dst, n Reg)   { a.word(0x0F10A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSxtl2SfromH(dst, n Reg)  { a.word(0x4F10A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUxtlSfromH(dst, n Reg)   { a.word(0x2F10A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUxtl2SfromH(dst, n Reg)  { a.word(0x6F10A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSxtlDfromS(dst, n Reg)   { a.word(0x0F20A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSxtl2DfromS(dst, n Reg)  { a.word(0x4F20A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUxtlDfromS(dst, n Reg)   { a.word(0x2F20A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUxtl2DfromS(dst, n Reg)  { a.word(0x6F20A400 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonMaddwd(dst, n, m Reg)    { a.neon3(0x4E209C00, 2, dst, n, m) }
func (a *Asm) NeonSmullHfromB(dst, n, m Reg) {
	a.word(0x0E20C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonSmull2HfromB(dst, n, m Reg) {
	a.word(0x4E20C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonUmullHfromB(dst, n, m Reg) {
	a.word(0x2E20C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonUmull2HfromB(dst, n, m Reg) {
	a.word(0x6E20C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonSmullSfromH(dst, n, m Reg) {
	a.word(0x0E60C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonSmull2SfromH(dst, n, m Reg) {
	a.word(0x4E60C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonUmullSfromH(dst, n, m Reg) {
	a.word(0x2E60C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonUmull2SfromH(dst, n, m Reg) {
	a.word(0x6E60C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonSmullDfromS(dst, n, m Reg) {
	a.word(0x0EA0C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonSmull2DfromS(dst, n, m Reg) {
	a.word(0x4EA0C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonUmullDfromS(dst, n, m Reg) {
	a.word(0x2EA0C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonUmull2DfromS(dst, n, m Reg) {
	a.word(0x6EA0C000 | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonSmullDQ(dst, n, m Reg)    { a.neon3(0x0E20C000, 4, dst, n, m) }
func (a *Asm) NeonUmullDQ(dst, n, m Reg)    { a.neon3(0x2E20C000, 4, dst, n, m) }
func (a *Asm) NeonSqxtnBfromH(dst, n Reg)   { a.word(0x0E214800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSqxtn2BfromH(dst, n Reg)  { a.word(0x4E214800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSqxtunBfromH(dst, n Reg)  { a.word(0x2E212800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSqxtun2BfromH(dst, n Reg) { a.word(0x6E212800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSqxtnHfromS(dst, n Reg)   { a.word(0x0E614800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSqxtn2HfromS(dst, n Reg)  { a.word(0x4E614800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSqxtunHfromS(dst, n Reg)  { a.word(0x2E612800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSqxtun2HfromS(dst, n Reg) { a.word(0x6E612800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonSqxtnSfromD(dst, n Reg)   { a.word(0x0EA14800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUqxtnSfromD(dst, n Reg)   { a.word(0x2EA14800 | r(n)<<5 | r(dst)) }

func (a *Asm) NeonAbsB(dst, n Reg) { a.word(0x4E20B800 | neonSize(1)<<22 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonAbsH(dst, n Reg) { a.word(0x4E20B800 | neonSize(2)<<22 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonAbsS(dst, n Reg) { a.word(0x4E20B800 | neonSize(4)<<22 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonAbsD(dst, n Reg) { a.word(0x4E20B800 | neonSize(8)<<22 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonNegB(dst, n Reg) { a.word(0x6E20B800 | neonSize(1)<<22 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonNegH(dst, n Reg) { a.word(0x6E20B800 | neonSize(2)<<22 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonNegS(dst, n Reg) { a.word(0x6E20B800 | neonSize(4)<<22 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonNegD(dst, n Reg) { a.word(0x6E20B800 | neonSize(8)<<22 | r(n)<<5 | r(dst)) }

func (a *Asm) NeonUshlB(dst, n, m Reg)  { a.neon3(0x6E204400, 1, dst, n, m) }
func (a *Asm) NeonUshlH(dst, n, m Reg)  { a.neon3(0x6E204400, 2, dst, n, m) }
func (a *Asm) NeonUshlS(dst, n, m Reg)  { a.neon3(0x6E204400, 4, dst, n, m) }
func (a *Asm) NeonUshlD(dst, n, m Reg)  { a.neon3(0x6E204400, 8, dst, n, m) }
func (a *Asm) NeonSshrvB(dst, n, m Reg) { a.neon3(0x4E204400, 1, dst, n, m) }
func (a *Asm) NeonSshrvH(dst, n, m Reg) { a.neon3(0x4E204400, 2, dst, n, m) }
func (a *Asm) NeonSshrvS(dst, n, m Reg) { a.neon3(0x4E204400, 4, dst, n, m) }
func (a *Asm) NeonUshrvB(dst, n, m Reg) { a.neon3(0x6E204400, 1, dst, n, m) }
func (a *Asm) NeonUshrvH(dst, n, m Reg) { a.neon3(0x6E204400, 2, dst, n, m) }
func (a *Asm) NeonUshrvS(dst, n, m Reg) { a.neon3(0x6E204400, 4, dst, n, m) }
func (a *Asm) NeonUshrvD(dst, n, m Reg) { a.neon3(0x6E204400, 8, dst, n, m) }

func (a *Asm) neonRightShift(base uint32, bytes int, dst, n Reg, shift uint8) {
	esize := uint32(bytes * 8)
	imm := 2*esize - uint32(shift)
	a.word(base | (imm&0x7F)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonSshrH(dst, n Reg, shift uint8) { a.neonRightShift(0x4F000400, 2, dst, n, shift) }
func (a *Asm) NeonSshrS(dst, n Reg, shift uint8) { a.neonRightShift(0x4F000400, 4, dst, n, shift) }
func (a *Asm) NeonUshrB(dst, n Reg, shift uint8) { a.neonRightShift(0x6F000400, 1, dst, n, shift) }
func (a *Asm) NeonUshrH(dst, n Reg, shift uint8) { a.neonRightShift(0x6F000400, 2, dst, n, shift) }
func (a *Asm) NeonUshrS(dst, n Reg, shift uint8) { a.neonRightShift(0x6F000400, 4, dst, n, shift) }
func (a *Asm) NeonUshrD(dst, n Reg, shift uint8) { a.neonRightShift(0x6F000400, 8, dst, n, shift) }

func (a *Asm) NeonZip1B(dst, n, m Reg) { a.neon3(0x4E003800, 1, dst, n, m) }
func (a *Asm) NeonZip1H(dst, n, m Reg) { a.neon3(0x4E003800, 2, dst, n, m) }
func (a *Asm) NeonZip1S(dst, n, m Reg) { a.neon3(0x4E003800, 4, dst, n, m) }
func (a *Asm) NeonZip2B(dst, n, m Reg) { a.neon3(0x4E007800, 1, dst, n, m) }
func (a *Asm) NeonZip2H(dst, n, m Reg) { a.neon3(0x4E007800, 2, dst, n, m) }
func (a *Asm) NeonZip2S(dst, n, m Reg) { a.neon3(0x4E007800, 4, dst, n, m) }

func neonImm5(bytes int, lane byte) uint32 {
	return (uint32(lane) << uint(neonSize(bytes)+1)) | (1 << neonSize(bytes))
}
func (a *Asm) NeonInsB(dst, rn Reg, lane byte) {
	a.word(0x4E001C00 | neonImm5(1, lane)<<16 | r(rn)<<5 | r(dst))
}
func (a *Asm) NeonInsH(dst, rn Reg, lane byte) {
	a.word(0x4E001C00 | neonImm5(2, lane)<<16 | r(rn)<<5 | r(dst))
}
func (a *Asm) NeonInsS(dst, rn Reg, lane byte) {
	a.word(0x4E001C00 | neonImm5(4, lane)<<16 | r(rn)<<5 | r(dst))
}
func (a *Asm) NeonInsD(dst, rn Reg, lane byte) {
	a.word(0x4E001C00 | neonImm5(8, lane)<<16 | r(rn)<<5 | r(dst))
}
func (a *Asm) NeonUmovB(rd, vn Reg, lane byte) {
	a.word(0x0E003C00 | neonImm5(1, lane)<<16 | r(vn)<<5 | r(rd))
}
func (a *Asm) NeonUmovH(rd, vn Reg, lane byte) {
	a.word(0x0E003C00 | neonImm5(2, lane)<<16 | r(vn)<<5 | r(rd))
}
func (a *Asm) NeonUmovS(rd, vn Reg, lane byte) {
	a.word(0x0E003C00 | neonImm5(4, lane)<<16 | r(vn)<<5 | r(rd))
}
func (a *Asm) NeonUmovD(rd, vn Reg, lane byte) {
	a.word(0x4E003C00 | neonImm5(8, lane)<<16 | r(vn)<<5 | r(rd))
}

func (a *Asm) NeonDupB(dst, src Reg)    { a.word(0x4E010400 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonDupH(dst, src Reg)    { a.word(0x4E020400 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonDupS(dst, src Reg)    { a.word(0x4E040400 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonDupD(dst, src Reg)    { a.word(0x4E080400 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonDupGprB(dst, src Reg) { a.word(0x4E010C00 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonDupGprH(dst, src Reg) { a.word(0x4E020C00 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonDupGprS(dst, src Reg) { a.word(0x4E040C00 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonDupGprD(dst, src Reg) { a.word(0x4E080C00 | r(src)<<5 | r(dst)) }
func (a *Asm) NeonDupLaneS(dst, src Reg, lane byte) {
	a.word(0x4E000400 | neonImm5(4, lane)<<16 | r(src)<<5 | r(dst))
}
func (a *Asm) NeonDupLaneD(dst, src Reg, lane byte) {
	a.word(0x4E000400 | neonImm5(8, lane)<<16 | r(src)<<5 | r(dst))
}
func (a *Asm) NeonInsLaneS(dst Reg, lane byte, src Reg) {
	a.word(0x6E000400 | neonImm5(4, lane)<<16 | r(src)<<5 | r(dst))
}
func (a *Asm) NeonInsLaneD(dst Reg, lane byte, src Reg) {
	a.word(0x6E000400 | neonImm5(8, lane)<<16 | r(src)<<5 | r(dst))
}
func (a *Asm) NeonTbl(dst, table, idx Reg) {
	a.word(0x4E000000 | r(idx)<<16 | r(table)<<5 | r(dst)) // TBL Vd.16b,{Vn.16b},Vm.16b
}
func (a *Asm) NeonPshufS(dst, src Reg, imm byte) {
	_ = imm
	if dst != src {
		a.NeonMov16b(dst, src)
	}
}
func (a *Asm) NeonMovemaskB(dst, src Reg) {
	a.FmovToGpr(dst, src, true)
	a.LsrImm(dst, dst, 7, true)
	a.AndImm32(dst, dst, 1)
}

func (a *Asm) NeonFadd(dst, n, m Reg, f64 bool) {
	a.word(fbase(f64, 0x4E20D400, 0x4E60D400) | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFsub(dst, n, m Reg, f64 bool) {
	a.word(fbase(f64, 0x4EA0D400, 0x4EE0D400) | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFmul(dst, n, m Reg, f64 bool) {
	a.word(fbase(f64, 0x6E20DC00, 0x6E60DC00) | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFdiv(dst, n, m Reg, f64 bool) {
	a.word(fbase(f64, 0x6E20FC00, 0x6E60FC00) | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFmax(dst, n, m Reg, f64 bool) {
	a.word(fbase(f64, 0x4E20F400, 0x4E60F400) | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFmin(dst, n, m Reg, f64 bool) {
	a.word(fbase(f64, 0x4EA0F400, 0x4EE0F400) | r(m)<<16 | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFabs(dst, n Reg, f64 bool) {
	a.word(fbase(f64, 0x4EA0F800, 0x4EE0F800) | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFneg(dst, n Reg, f64 bool) {
	a.word(fbase(f64, 0x6EA0F800, 0x6EE0F800) | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFsqrt(dst, n Reg, f64 bool) {
	a.word(fbase(f64, 0x6EA1F800, 0x6EE1F800) | r(n)<<5 | r(dst))
}
func (a *Asm) NeonFcvtnSfromD(dst, n Reg)  { a.word(0x0E616800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonFcvtlDfromS(dst, n Reg)  { a.word(0x0E617800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonScvtfSfromS(dst, n Reg)  { a.word(0x4E21D800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUcvtfSfromS(dst, n Reg)  { a.word(0x6E21D800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonScvtfDfromD(dst, n Reg)  { a.word(0x4E61D800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonUcvtfDfromD(dst, n Reg)  { a.word(0x6E61D800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonFcvtzsSfromS(dst, n Reg) { a.word(0x4EA1B800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonFcvtzuSfromS(dst, n Reg) { a.word(0x6EA1B800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonFcvtzsDfromD(dst, n Reg) { a.word(0x4EE1B800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonFcvtzuDfromD(dst, n Reg) { a.word(0x6EE1B800 | r(n)<<5 | r(dst)) }
func (a *Asm) NeonFrint(dst, n Reg, f64 bool, mode byte) {
	switch mode {
	case 'n':
		a.word(fbase(f64, 0x4E218800, 0x4E618800) | r(n)<<5 | r(dst))
	case 'm':
		a.word(fbase(f64, 0x4E219800, 0x4E619800) | r(n)<<5 | r(dst))
	case 'p':
		a.word(fbase(f64, 0x4EA18800, 0x4EE18800) | r(n)<<5 | r(dst))
	case 'z':
		a.word(fbase(f64, 0x4EA19800, 0x4EE19800) | r(n)<<5 | r(dst))
	default:
		panic("arm64: invalid neon frint mode")
	}
}
func (a *Asm) NeonFcmp(dst, n, m Reg, f64 bool, pred byte) {
	switch pred {
	case 0x00: // eq
		a.word(fbase(f64, 0x4E20E400, 0x4E60E400) | r(m)<<16 | r(n)<<5 | r(dst))
	case 0x11: // lt
		a.word(fbase(f64, 0x6EA0E400, 0x6EE0E400) | r(n)<<16 | r(m)<<5 | r(dst))
	case 0x12: // le
		a.word(fbase(f64, 0x6E20E400, 0x6E60E400) | r(n)<<16 | r(m)<<5 | r(dst))
	case 0x1d: // ge
		a.word(fbase(f64, 0x6E20E400, 0x6E60E400) | r(m)<<16 | r(n)<<5 | r(dst))
	case 0x1e: // gt
		a.word(fbase(f64, 0x6EA0E400, 0x6EE0E400) | r(m)<<16 | r(n)<<5 | r(dst))
	default:
		// neq/unordered placeholder: invert eq with all-ones mask using dst as temp.
		a.word(fbase(f64, 0x4E20E400, 0x4E60E400) | r(m)<<16 | r(n)<<5 | r(dst))
	}
}

// LdrF / StrF load/store a scalar float spill slot (always the 64-bit D form —
// the frame slot is 8 bytes; an f32 lives in the low 32).
func (a *Asm) LdrF(rt, base Reg, disp int32, f64 bool) {
	if f64 {
		a.LdrD(rt, base, disp)
	} else {
		a.LdrS(rt, base, disp)
	}
}
func (a *Asm) StrF(base Reg, disp int32, rt Reg, f64 bool) {
	if f64 {
		a.StrD(base, disp, rt)
	} else {
		a.StrS(base, disp, rt)
	}
}

// MovImm32 materializes a 32-bit constant into the low half of rd (upper zero).
func (a *Asm) MovImm32(rd Reg, val int32) { a.MovImm64(rd, uint64(uint32(val))) }
