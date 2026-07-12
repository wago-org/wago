package amd64

// SSE/scalar-float encoders. XMM registers reuse Reg indices 0..15.

func sdPrefix(f64 bool) byte {
	if f64 {
		return 0xF2
	}
	return 0xF3
}

func (a *Asm) sseRR(prefix, op byte, reg, rm Reg, w bool) {
	if prefix != 0 {
		a.emit(prefix)
	}
	if w || reg >= 8 || rm >= 8 {
		a.emit(rex(w, reg >= 8, false, rm >= 8))
	}
	a.emit(0x0F, op, 0xC0|((byte(reg)&7)<<3)|byte(rm&7))
}

func (a *Asm) sseRRI(prefix byte, op []byte, reg, rm Reg, w bool, imm byte) {
	if prefix != 0 {
		a.emit(prefix)
	}
	if w || reg >= 8 || rm >= 8 {
		a.emit(rex(w, reg >= 8, false, rm >= 8))
	}
	a.emit(op...)
	a.emit(0xC0|((byte(reg)&7)<<3)|byte(rm&7), imm)
}

// SseRR exposes the raw two-operand SSE reg,reg encoder for op bytes without a
// dedicated helper (e.g. orps/andps/xorps used by float min/max/neg/copysign).
func (a *Asm) SseRR(prefix, op byte, reg, rm Reg, w bool) { a.sseRR(prefix, op, reg, rm, w) }

func (a *Asm) FAdd(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x58, dst, src, false) }
func (a *Asm) FSub(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5C, dst, src, false) }
func (a *Asm) FMul(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x59, dst, src, false) }
func (a *Asm) FDiv(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5E, dst, src, false) }
func (a *Asm) FMin(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5D, dst, src, false) }
func (a *Asm) FMax(dst, src Reg, f64 bool)  { a.sseRR(sdPrefix(f64), 0x5F, dst, src, false) }
func (a *Asm) FSqrt(dst, src Reg, f64 bool) { a.sseRR(sdPrefix(f64), 0x51, dst, src, false) }

func (a *Asm) FMov(dst, src Reg, f64 bool) { a.sseRR(sdPrefix(f64), 0x10, dst, src, false) }

// --- VEX 3-operand (AVX) forms -------------------------------------------------
//
// The non-destructive `dst = op(src1, src2)` encoding lets a float op read both
// operands directly and write a distinct destination, avoiding the movsd-to-scratch
// that legacy 2-operand SSE needs to preserve an operand. wago already emits
// LZCNT/TZCNT (BMI1/ABM, ~2013), which is newer than AVX (2011), so this raises no
// effective ISA baseline. Always uses the 3-byte VEX form (0xC4) so every xmm0-15
// combination encodes uniformly.

// vexPP is the VEX pp field for scalar F2 (f64) / F3 (f32) ops.
func vexPP(f64 bool) byte {
	if f64 {
		return 0b11 // F2
	}
	return 0b10 // F3
}

const (
	vexMap0F   byte = 0b00001
	vexMap0F38 byte = 0b00010
	vexMap0F3A byte = 0b00011
)

// vex3RRR emits a 3-byte-VEX 0F-map op in 3-operand register form: reg=dst,
// vvvv=src1, rm=src2. pp selects the implied legacy prefix (0=none,1=66,2=F3,3=F2).
func (a *Asm) vex3RRR(pp, op byte, dst, src1, src2 Reg) {
	a.vex3RRRMap(vexMap0F, pp, op, dst, src1, src2)
}

// vex3RRRMap emits a 3-byte VEX.128 register form: reg=dst, vvvv=src1,
// rm=src2. opcodeMap is the VEX m-mmmm field (1=0F, 2=0F38, 3=0F3A).
// pp selects the implied legacy prefix (0=none, 1=66, 2=F3, 3=F2). W=0 and L=0
// are fixed because wago's SIMD baseline uses 128-bit XMM encodings here.
func (a *Asm) vex3RRRMap(opcodeMap, pp, op byte, dst, src1, src2 Reg) {
	rBit, bBit := byte(1), byte(1) // inverted REX.R / REX.B
	if dst >= 8 {
		rBit = 0
	}
	if src2 >= 8 {
		bBit = 0
	}
	byte1 := (rBit << 7) | (1 << 6) | (bBit << 5) | (opcodeMap & 0x1F) // X̄=1
	vvvv := (^byte(src1)) & 0x0F
	byte2 := (vvvv << 3) | (pp & 0x03) // W=0, L=0 (128)
	a.emit(0xC4, byte1, byte2, op, 0xC0|((byte(dst)&7)<<3)|byte(src2&7))
}

func (a *Asm) vex3RRIMap(opcodeMap, pp, op byte, dst, src1, src2 Reg, imm byte) {
	a.vex3RRRMap(opcodeMap, pp, op, dst, src1, src2)
	a.emit(imm)
}

// vex3RRReserved emits a VEX.128 register form whose vvvv field is reserved and
// must be 1111b (for example vpmovmskb). reg selects ModRM.reg; rm selects
// ModRM.r/m.
func (a *Asm) vex3RRReserved(opcodeMap, pp, op byte, reg, rm Reg) {
	rBit, bBit := byte(1), byte(1) // inverted REX.R / REX.B
	if reg >= 8 {
		rBit = 0
	}
	if rm >= 8 {
		bBit = 0
	}
	byte1 := (rBit << 7) | (1 << 6) | (bBit << 5) | (opcodeMap & 0x1F) // X̄=1
	byte2 := byte(0x78) | (pp & 0x03)                                  // vvvv=1111, W=0, L=0 (128)
	a.emit(0xC4, byte1, byte2, op, 0xC0|((byte(reg)&7)<<3)|byte(rm&7))
}

func (a *Asm) vex3MemPrefix(opcodeMap, pp byte, reg Reg, src1 Reg, hasSrc1 bool, base Reg, index Reg, indexed bool) {
	rBit, xBit, bBit := byte(1), byte(1), byte(1) // inverted REX.R / REX.X / REX.B
	if reg >= 8 {
		rBit = 0
	}
	if indexed && index >= 8 {
		xBit = 0
	}
	if base >= 8 {
		bBit = 0
	}
	byte1 := (rBit << 7) | (xBit << 6) | (bBit << 5) | (opcodeMap & 0x1F)
	vvvv := byte(0x0F) // reserved vvvv=1111 for two-operand VEX memory moves
	if hasSrc1 {
		vvvv = (^byte(src1)) & 0x0F
	}
	byte2 := (vvvv << 3) | (pp & 0x03) // W=0, L=0 (128)
	a.emit(0xC4, byte1, byte2)
}

func (a *Asm) vex3MemDisp(opcodeMap, pp, op byte, reg Reg, src1 Reg, hasSrc1 bool, base Reg, disp int32) {
	a.vex3MemPrefix(opcodeMap, pp, reg, src1, hasSrc1, base, 0, false)
	a.emit(op)
	if base&7 == 4 { // RSP/R12 base: rm=100 means "SIB follows"
		a.emit(0x80|((byte(reg)&7)<<3)|0x04, 0x24) // mod=10 disp32, SIB=base only
	} else {
		a.emit(0x80 | ((byte(reg) & 7) << 3) | byte(base&7)) // mod=10 disp32
	}
	a.imm32(disp)
}

func (a *Asm) vex3MemIdx(opcodeMap, pp, op byte, reg Reg, src1 Reg, hasSrc1 bool, base, index Reg, disp int32) {
	a.vex3MemPrefix(opcodeMap, pp, reg, src1, hasSrc1, base, index, true)
	a.emit(op)
	a.sibAddr(reg, base, index, disp)
}

// Scalar float arithmetic, 3-operand: dst = src1 <op> src2.
func (a *Asm) VFAdd(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(vexPP(f64), 0x58, dst, s1, s2) }
func (a *Asm) VFSub(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(vexPP(f64), 0x5C, dst, s1, s2) }
func (a *Asm) VFMul(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(vexPP(f64), 0x59, dst, s1, s2) }
func (a *Asm) VFDiv(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(vexPP(f64), 0x5E, dst, s1, s2) }

func packedPP(f64 bool) byte {
	if f64 {
		return 0b01 // 66 = packed double
	}
	return 0b00 // packed single has no implied prefix
}

// Packed float arithmetic, VEX.128 3-operand: dst = src1 <op> src2.
func (a *Asm) VFPackedAdd(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(packedPP(f64), 0x58, dst, s1, s2) }
func (a *Asm) VFPackedSub(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(packedPP(f64), 0x5C, dst, s1, s2) }
func (a *Asm) VFPackedMul(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(packedPP(f64), 0x59, dst, s1, s2) }
func (a *Asm) VFPackedDiv(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(packedPP(f64), 0x5E, dst, s1, s2) }
func (a *Asm) VFPackedMin(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(packedPP(f64), 0x5D, dst, s1, s2) }
func (a *Asm) VFPackedMax(dst, s1, s2 Reg, f64 bool) { a.vex3RRR(packedPP(f64), 0x5F, dst, s1, s2) }
func (a *Asm) VFPackedSqrt(dst, src Reg, f64 bool) {
	a.vex3RRReserved(vexMap0F, packedPP(f64), 0x51, dst, src)
}

// VFRoundPacked emits VROUNDPS/VROUNDPD (SSE4.1 through VEX.128) with a raw
// x86 rounding-mode immediate. Wasm rounding semantics stay in the backend.
func (a *Asm) VFRoundPacked(dst, src Reg, f64 bool, imm byte) {
	op := byte(0x08) // vroundps
	if f64 {
		op = 0x09 // vroundpd
	}
	a.vex3RRReserved(vexMap0F3A, 0b01, op, dst, src)
	a.emit(imm)
}

// Vcvttps2dq emits VCVTTPS2DQ (packed single-precision float to signed dword,
// truncating). Wasm relaxation and saturation semantics stay in the backend.
func (a *Asm) Vcvttps2dq(dst, src Reg) { a.vex3RRReserved(vexMap0F, 0b10, 0x5B, dst, src) }

// Vcvttpd2dq emits VCVTTPD2DQ (packed double-precision float to signed dword,
// truncating). Wasm relaxation and saturation semantics stay in the backend.
func (a *Asm) Vcvttpd2dq(dst, src Reg) { a.vex3RRReserved(vexMap0F, 0b01, 0xE6, dst, src) }

// Packed integer<->float and float<->float conversions (VEX.128, 2-operand).
// Rounding follows MXCSR (round-to-nearest-even by default = Wasm), and NaN
// propagation matches the scalar CVT* ops these replace.
func (a *Asm) Vcvtdq2ps(dst, src Reg) { a.vex3RRReserved(vexMap0F, 0b00, 0x5B, dst, src) } // i32x4 -> f32x4 (signed)
func (a *Asm) Vcvtdq2pd(dst, src Reg) { a.vex3RRReserved(vexMap0F, 0b10, 0xE6, dst, src) } // low i32x2 -> f64x2 (signed)
func (a *Asm) Vcvtps2pd(dst, src Reg) { a.vex3RRReserved(vexMap0F, 0b00, 0x5A, dst, src) } // low f32x2 -> f64x2 (promote)
func (a *Asm) Vcvtpd2ps(dst, src Reg) { a.vex3RRReserved(vexMap0F, 0b01, 0x5A, dst, src) } // f64x2 -> low f32x2, upper zeroed (demote)

// VFCmpPacked emits VCMPS/PD with a raw x86 predicate immediate. It is kept
// predicate-agnostic so the backend owns Wasm comparison semantics.
// VShufps emits VSHUFPS: dst = 4x32 shuffle selecting two dwords from s1 and two
// from s2 per the imm8 control. x86 helper only.
func (a *Asm) VShufps(dst, s1, s2 Reg, imm byte) {
	a.vex3RRIMap(vexMap0F, 0b00, 0xC6, dst, s1, s2, imm)
}

func (a *Asm) VFCmpPacked(dst, s1, s2 Reg, f64 bool, imm byte) {
	a.vex3RRIMap(vexMap0F, packedPP(f64), 0xC2, dst, s1, s2, imm)
}

// VSseRRR is the 3-operand form of SseRR (for the packed-logical ops andps/pd,
// orps/pd, xorps/pd used by neg/abs/copysign): dst = src1 <op> src2. pp is the
// legacy prefix code (0 = none = ps, 1 = 66 = pd).
func (a *Asm) VSseRRR(pp, op byte, dst, s1, s2 Reg) { a.vex3RRR(pp, op, dst, s1, s2) }

// Packed 128-bit lane shuffle/insert/extract helpers used by Wasm SIMD lowering.
// They expose x86 instructions only; lane validity and Wasm semantics stay in the
// backend. These legacy SSE/SSE4.1 encodings are within wago's linux/amd64 SIMD
// baseline and avoid AVX2 broadcast requirements.
func (a *Asm) Pshufd(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0x70}, dst, src, false, imm)
}
func (a *Asm) Pshuflw(dst, src Reg, imm byte) {
	a.sseRRI(0xF2, []byte{0x0F, 0x70}, dst, src, false, imm)
}
func (a *Asm) Punpcklqdq(dst, src Reg) { a.sseRR(0x66, 0x6C, dst, src, false) }
func (a *Asm) Pinsrb(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0x3A, 0x20}, dst, src, false, imm)
}
func (a *Asm) Pinsrw(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0xC4}, dst, src, false, imm)
}
func (a *Asm) Pinsrd(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0x3A, 0x22}, dst, src, false, imm)
}
func (a *Asm) Pinsrq(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0x3A, 0x22}, dst, src, true, imm)
}
func (a *Asm) Pextrb(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0x3A, 0x14}, src, dst, false, imm)
}
func (a *Asm) Pextrw(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0xC5}, dst, src, false, imm)
}
func (a *Asm) Pextrd(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0x3A, 0x16}, src, dst, false, imm)
}
func (a *Asm) Pextrq(dst, src Reg, imm byte) {
	a.sseRRI(0x66, []byte{0x0F, 0x3A, 0x16}, src, dst, true, imm)
}
func (a *Asm) Pmovmskb(dst, src Reg) {
	a.sseRR(0x66, 0xD7, dst, src, false)
}

// Packed 128-bit integer SIMD VEX helpers. These expose x86 instructions used by
// Wasm SIMD lowering while keeping Wasm-specific semantics in the backend.
func (a *Asm) VMovdquLoadDisp(dst, base Reg, disp int32) {
	a.vex3MemDisp(vexMap0F, 0b10, 0x6F, dst, 0, false, base, disp)
}
func (a *Asm) VMovdquStoreDisp(base Reg, disp int32, src Reg) {
	a.vex3MemDisp(vexMap0F, 0b10, 0x7F, src, 0, false, base, disp)
}
func (a *Asm) VMovdquLoadIdx(dst, base, index Reg, disp int32) {
	a.vex3MemIdx(vexMap0F, 0b10, 0x6F, dst, 0, false, base, index, disp)
}
func (a *Asm) VMovdquStoreIdx(base, index, src Reg, disp int32) {
	a.vex3MemIdx(vexMap0F, 0b10, 0x7F, src, 0, false, base, index, disp)
}
func (a *Asm) VMovdqu(dst, src Reg) {
	a.vex3RRReserved(vexMap0F, 0b10, 0x6F, dst, src)
}

func (a *Asm) VPaddb(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xFC, dst, s1, s2) }
func (a *Asm) VPaddw(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xFD, dst, s1, s2) }
func (a *Asm) VPaddd(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xFE, dst, s1, s2) }
func (a *Asm) VPaddq(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xD4, dst, s1, s2) }
func (a *Asm) VPaddsb(dst, s1, s2 Reg)   { a.vex3RRR(0b01, 0xEC, dst, s1, s2) }
func (a *Asm) VPaddusb(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0xDC, dst, s1, s2) }
func (a *Asm) VPaddsw(dst, s1, s2 Reg)   { a.vex3RRR(0b01, 0xED, dst, s1, s2) }
func (a *Asm) VPaddusw(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0xDD, dst, s1, s2) }
func (a *Asm) VPsubb(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xF8, dst, s1, s2) }
func (a *Asm) VPsubw(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xF9, dst, s1, s2) }
func (a *Asm) VPsubd(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xFA, dst, s1, s2) }
func (a *Asm) VPsubq(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xFB, dst, s1, s2) }
func (a *Asm) VPsubsb(dst, s1, s2 Reg)   { a.vex3RRR(0b01, 0xE8, dst, s1, s2) }
func (a *Asm) VPsubusb(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0xD8, dst, s1, s2) }
func (a *Asm) VPsubsw(dst, s1, s2 Reg)   { a.vex3RRR(0b01, 0xE9, dst, s1, s2) }
func (a *Asm) VPsubusw(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0xD9, dst, s1, s2) }
func (a *Asm) VPand(dst, s1, s2 Reg)     { a.vex3RRR(0b01, 0xDB, dst, s1, s2) }
func (a *Asm) VPandn(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xDF, dst, s1, s2) }
func (a *Asm) VPor(dst, s1, s2 Reg)      { a.vex3RRR(0b01, 0xEB, dst, s1, s2) }
func (a *Asm) VPxor(dst, s1, s2 Reg)     { a.vex3RRR(0b01, 0xEF, dst, s1, s2) }
func (a *Asm) VPcmpeqb(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0x74, dst, s1, s2) }
func (a *Asm) VPcmpeqw(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0x75, dst, s1, s2) }
func (a *Asm) VPcmpeqd(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0x76, dst, s1, s2) }
func (a *Asm) VPcmpeqq(dst, s1, s2 Reg)  { a.vex3RRRMap(vexMap0F38, 0b01, 0x29, dst, s1, s2) }
func (a *Asm) VPcmpgtb(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0x64, dst, s1, s2) }
func (a *Asm) VPcmpgtw(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0x65, dst, s1, s2) }
func (a *Asm) VPcmpgtd(dst, s1, s2 Reg)  { a.vex3RRR(0b01, 0x66, dst, s1, s2) }
func (a *Asm) VPcmpgtq(dst, s1, s2 Reg)  { a.vex3RRRMap(vexMap0F38, 0b01, 0x37, dst, s1, s2) }
func (a *Asm) VPmovmskb(dst, src Reg)    { a.vex3RRReserved(vexMap0F, 0b01, 0xD7, dst, src) }
func (a *Asm) VMovmskps(dst, src Reg)    { a.vex3RRReserved(vexMap0F, 0b00, 0x50, dst, src) } // 4 f32-lane sign bits -> gpr
func (a *Asm) VMovmskpd(dst, src Reg)    { a.vex3RRReserved(vexMap0F, 0b01, 0x50, dst, src) } // 2 f64-lane sign bits -> gpr
func (a *Asm) VPacksswb(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x63, dst, s1, s2) }               // pack words->bytes, signed saturate
func (a *Asm) VPabsb(dst, src Reg)       { a.vex3RRReserved(vexMap0F38, 0b01, 0x1C, dst, src) }
func (a *Asm) VPabsw(dst, src Reg)       { a.vex3RRReserved(vexMap0F38, 0b01, 0x1D, dst, src) }
func (a *Asm) VPabsd(dst, src Reg)       { a.vex3RRReserved(vexMap0F38, 0b01, 0x1E, dst, src) }

// VPsllw/VPsrlw/VPsraw emit variable-count packed 16-bit lane shifts. They are
// x86 helpers only; Wasm count masking stays in the backend.
func (a *Asm) VPsllw(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xF1, dst, s1, s2) }
func (a *Asm) VPsrlw(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xD1, dst, s1, s2) }
func (a *Asm) VPsraw(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xE1, dst, s1, s2) }

// VPslld/VPsrld/VPsrad emit variable-count packed 32-bit lane shifts. They are
// x86 helpers only; Wasm count masking stays in the backend.
func (a *Asm) VPslld(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xF2, dst, s1, s2) }
func (a *Asm) VPsrld(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xD2, dst, s1, s2) }
func (a *Asm) VPsrad(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xE2, dst, s1, s2) }

// VPsllq/VPsrlq emit variable-count packed 64-bit lane logical shifts. They are
// x86 helpers only; Wasm count masking stays in the backend.
func (a *Asm) VPsllq(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xF3, dst, s1, s2) }
func (a *Asm) VPsrlq(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xD3, dst, s1, s2) }

// VPsrlwImm emits the immediate logical right shift of packed 16-bit lanes.
// This is an x86 helper only; Wasm lane-count semantics stay in the backend.
func (a *Asm) VPsrlwImm(dst, src Reg, imm byte) {
	a.vexShiftWordImm(2, dst, src, imm)
}

// VPsllwImm emits the immediate logical left shift of packed 16-bit lanes (/6).
func (a *Asm) VPsllwImm(dst, src Reg, imm byte) {
	a.vexShiftWordImm(6, dst, src, imm)
}

// VPsrawImm emits the immediate arithmetic right shift of packed 16-bit lanes.
// This is an x86 helper only; Wasm lane-count semantics stay in the backend.
func (a *Asm) VPsrawImm(dst, src Reg, imm byte) {
	a.vexShiftWordImm(4, dst, src, imm)
}

// VPsradImm emits the immediate arithmetic right shift of packed 32-bit lanes.
// This is an x86 helper only; Wasm lane-count semantics stay in the backend.
func (a *Asm) VPsradImm(dst, src Reg, imm byte) {
	a.vexShiftDwordImm(4, dst, src, imm)
}

// VPsrldImm emits the immediate logical right shift of packed 32-bit lanes.
// This is an x86 helper only; Wasm lane-count semantics stay in the backend.
func (a *Asm) VPsrldImm(dst, src Reg, imm byte) {
	a.vexShiftDwordImm(2, dst, src, imm)
}

// VPsrlqImm emits the immediate logical right shift of packed 64-bit lanes.
// This is an x86 helper only; Wasm lane-count semantics stay in the backend.
func (a *Asm) VPsrlqImm(dst, src Reg, imm byte) {
	a.vexShiftQwordImm(2, dst, src, imm)
}

// VPslldImm/VPsllqImm emit immediate logical LEFT shifts of packed 32/64-bit
// lanes (ModRM.reg extension 6). Used to build sign-bit masks in-register.
func (a *Asm) VPslldImm(dst, src Reg, imm byte) { a.vexShiftDwordImm(6, dst, src, imm) }
func (a *Asm) VPsllqImm(dst, src Reg, imm byte) { a.vexShiftQwordImm(6, dst, src, imm) }

func (a *Asm) vexShiftQwordImm(ext byte, dst, src Reg, imm byte) {
	rBit, bBit := byte(1), byte(1) // inverted REX.R / REX.B; ModRM.reg is the fixed opcode extension.
	if src >= 8 {
		bBit = 0
	}
	byte1 := (rBit << 7) | (1 << 6) | (bBit << 5) | (vexMap0F & 0x1F) // X̄=1
	vvvv := (^byte(dst)) & 0x0F
	byte2 := (vvvv << 3) | 0b01 // W=0, L=0, pp=66
	a.emit(0xC4, byte1, byte2, 0x73, 0xC0|((ext&7)<<3)|byte(src&7), imm)
}

func (a *Asm) vexShiftWordImm(ext byte, dst, src Reg, imm byte) {
	rBit, bBit := byte(1), byte(1) // inverted REX.R / REX.B; ModRM.reg is the fixed opcode extension.
	if src >= 8 {
		bBit = 0
	}
	byte1 := (rBit << 7) | (1 << 6) | (bBit << 5) | (vexMap0F & 0x1F) // X̄=1
	vvvv := (^byte(dst)) & 0x0F
	byte2 := (vvvv << 3) | 0b01 // W=0, L=0, pp=66
	a.emit(0xC4, byte1, byte2, 0x71, 0xC0|((ext&7)<<3)|byte(src&7), imm)
}

func (a *Asm) vexShiftDwordImm(ext byte, dst, src Reg, imm byte) {
	rBit, bBit := byte(1), byte(1) // inverted REX.R / REX.B; ModRM.reg is the fixed opcode extension.
	if src >= 8 {
		bBit = 0
	}
	byte1 := (rBit << 7) | (1 << 6) | (bBit << 5) | (vexMap0F & 0x1F) // X̄=1
	vvvv := (^byte(dst)) & 0x0F
	byte2 := (vvvv << 3) | 0b01 // W=0, L=0, pp=66
	a.emit(0xC4, byte1, byte2, 0x72, 0xC0|((ext&7)<<3)|byte(src&7), imm)
}

func (a *Asm) VPadddMemDisp(dst, s1, base Reg, disp int32) {
	a.vex3MemDisp(vexMap0F, 0b01, 0xFE, dst, s1, true, base, disp)
}

func (a *Asm) VPshufb(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x00, dst, s1, s2) }
func (a *Asm) VPhaddw(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x01, dst, s1, s2) }
func (a *Asm) VPhaddd(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x02, dst, s1, s2) }
func (a *Asm) VPmulhrsw(dst, s1, s2 Reg)  { a.vex3RRRMap(vexMap0F38, 0b01, 0x0B, dst, s1, s2) }
func (a *Asm) VPunpcklbw(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x60, dst, s1, s2) }
func (a *Asm) VPunpcklwd(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x61, dst, s1, s2) }
func (a *Asm) VPunpckldq(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x62, dst, s1, s2) }
func (a *Asm) VPunpckhbw(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x68, dst, s1, s2) }
func (a *Asm) VPunpckhwd(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x69, dst, s1, s2) }
func (a *Asm) VPunpckhdq(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x6A, dst, s1, s2) }
func (a *Asm) VPpacksswb(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x63, dst, s1, s2) }
func (a *Asm) VPpackssdw(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x6B, dst, s1, s2) }
func (a *Asm) VPpackuswb(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0x67, dst, s1, s2) }
func (a *Asm) VPpackusdw(dst, s1, s2 Reg) { a.vex3RRRMap(vexMap0F38, 0b01, 0x2B, dst, s1, s2) }
func (a *Asm) VPmaddwd(dst, s1, s2 Reg)   { a.vex3RRR(0b01, 0xF5, dst, s1, s2) }
func (a *Asm) VPmaddubsw(dst, s1, s2 Reg) { a.vex3RRRMap(vexMap0F38, 0b01, 0x04, dst, s1, s2) }
func (a *Asm) VPmullw(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xD5, dst, s1, s2) }
func (a *Asm) VPminsb(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x38, dst, s1, s2) }
func (a *Asm) VPminub(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xDA, dst, s1, s2) }
func (a *Asm) VPmaxsb(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x3C, dst, s1, s2) }
func (a *Asm) VPmaxub(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xDE, dst, s1, s2) }
func (a *Asm) VPavgb(dst, s1, s2 Reg)     { a.vex3RRR(0b01, 0xE0, dst, s1, s2) }
func (a *Asm) VPminsw(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xEA, dst, s1, s2) }
func (a *Asm) VPminuw(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x3A, dst, s1, s2) }
func (a *Asm) VPmaxsw(dst, s1, s2 Reg)    { a.vex3RRR(0b01, 0xEE, dst, s1, s2) }
func (a *Asm) VPmaxuw(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x3E, dst, s1, s2) }
func (a *Asm) VPavgw(dst, s1, s2 Reg)     { a.vex3RRR(0b01, 0xE3, dst, s1, s2) }
func (a *Asm) VPminsd(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x39, dst, s1, s2) }
func (a *Asm) VPminud(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x3B, dst, s1, s2) }
func (a *Asm) VPmaxsd(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x3D, dst, s1, s2) }
func (a *Asm) VPmaxud(dst, s1, s2 Reg)    { a.vex3RRRMap(vexMap0F38, 0b01, 0x3F, dst, s1, s2) }
func (a *Asm) VPshufbMemIdx(dst, s1, base, index Reg, disp int32) {
	a.vex3MemIdx(vexMap0F38, 0b01, 0x00, dst, s1, true, base, index, disp)
}
func (a *Asm) VPmulld(dst, s1, s2 Reg)  { a.vex3RRRMap(vexMap0F38, 0b01, 0x40, dst, s1, s2) }
func (a *Asm) VPmuldq(dst, s1, s2 Reg)  { a.vex3RRRMap(vexMap0F38, 0b01, 0x28, dst, s1, s2) }
func (a *Asm) VPmuludq(dst, s1, s2 Reg) { a.vex3RRR(0b01, 0xF4, dst, s1, s2) }
func (a *Asm) VPblendw(dst, s1, s2 Reg, imm byte) {
	a.vex3RRIMap(vexMap0F3A, 0b01, 0x0E, dst, s1, s2, imm)
}

// Round emits ROUNDSS/ROUNDSD (SSE4.1): dst = round(src) using rounding-mode
// imm8 (bits 0-1 select nearest/floor/ceil/trunc; bit 3 suppresses precision).
func (a *Asm) Round(dst, src Reg, f64 bool, mode byte) {
	a.emit(0x66)
	if dst >= 8 || src >= 8 {
		a.emit(rex(false, dst >= 8, false, src >= 8))
	}
	op := byte(0x0A) // roundss
	if f64 {
		op = 0x0B // roundsd
	}
	a.emit(0x0F, 0x3A, op, 0xC0|((byte(dst)&7)<<3)|byte(src&7), mode)
}

func (a *Asm) Ucomis(dst, src Reg, f64 bool) {
	var p byte
	if f64 {
		p = 0x66
	}
	a.sseRR(p, 0x2E, dst, src, false)
}

func (a *Asm) Cvtss2sd(dst, src Reg) { a.sseRR(0xF3, 0x5A, dst, src, false) }
func (a *Asm) Cvtsd2ss(dst, src Reg) { a.sseRR(0xF2, 0x5A, dst, src, false) }

func (a *Asm) Cvtsi2f(xmm, gpr Reg, f64, w bool) { a.sseRR(sdPrefix(f64), 0x2A, xmm, gpr, w) }

func (a *Asm) Cvttf2si(gpr, xmm Reg, f64, w bool) { a.sseRR(sdPrefix(f64), 0x2C, gpr, xmm, w) }

func (a *Asm) MovGprToXmm(xmm, gpr Reg, w bool) { a.sseRR(0x66, 0x6E, xmm, gpr, w) }

func (a *Asm) MovXmmToGpr(gpr, xmm Reg, w bool) { a.sseRR(0x66, 0x7E, xmm, gpr, w) }

func (a *Asm) fmemDisp(op byte, xmm, base Reg, disp int32, f64 bool) {
	a.emit(sdPrefix(f64))
	if xmm >= 8 || base >= 8 {
		a.emit(rex(false, xmm >= 8, false, base >= 8))
	}
	a.emit(0x0F, op)
	if base&7 == 4 { // RSP/R12 base: rm=100 means "SIB follows"
		a.emit(0x80|((byte(xmm)&7)<<3)|0x04, 0x24) // mod=10 disp32, SIB=base only
	} else {
		a.emit(0x80 | ((byte(xmm) & 7) << 3) | byte(base&7)) // mod=10 disp32
	}
	a.imm32(disp)
}

func (a *Asm) FLoadDisp(xmm, base Reg, disp int32, f64 bool) { a.fmemDisp(0x10, xmm, base, disp, f64) }
func (a *Asm) FStoreDisp(base Reg, disp int32, xmm Reg, f64 bool) {
	a.fmemDisp(0x11, xmm, base, disp, f64)
}

func (a *Asm) fmemIdx(op byte, xmm, base, index Reg, disp int32, f64 bool) {
	a.emit(sdPrefix(f64))
	if xmm >= 8 || index >= 8 || base >= 8 {
		a.emit(rex(false, xmm >= 8, index >= 8, base >= 8))
	}
	a.emit(0x0F, op)
	a.sibAddr(xmm, base, index, disp)
}

func (a *Asm) FLoadIdx(xmm, base, index Reg, disp int32, f64 bool) {
	a.fmemIdx(0x10, xmm, base, index, disp, f64)
}
func (a *Asm) FStoreIdx(base, index, xmm Reg, disp int32, f64 bool) {
	a.fmemIdx(0x11, xmm, base, index, disp, f64)
}

// SseIdx emits a raw SSE reg, [base+index+disp] instruction. It is used for
// scalar float ALU memory folds (addss/addsd/etc.) and packed logical/min/max
// forms where the caller chooses the exact legacy prefix.
func (a *Asm) SseIdx(prefix, op byte, xmm, base, index Reg, disp int32) {
	if prefix != 0 {
		a.emit(prefix)
	}
	if xmm >= 8 || index >= 8 || base >= 8 {
		a.emit(rex(false, xmm >= 8, index >= 8, base >= 8))
	}
	a.emit(0x0F, op)
	a.sibAddr(xmm, base, index, disp)
}
