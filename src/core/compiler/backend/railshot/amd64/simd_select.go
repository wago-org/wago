//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

const (
	opVPacksswb  simdBinaryOp = 0x63
	opVPaddb     simdBinaryOp = 0xFC
	opVPaddd     simdBinaryOp = 0xFE
	opVPaddq     simdBinaryOp = 0xD4
	opVPaddsb    simdBinaryOp = 0xEC
	opVPaddsw    simdBinaryOp = 0xED
	opVPaddusb   simdBinaryOp = 0xDC
	opVPaddusw   simdBinaryOp = 0xDD
	opVPaddw     simdBinaryOp = 0xFD
	opVPand      simdBinaryOp = 0xDB
	opVPandn     simdBinaryOp = 0xDF
	opVPavgb     simdBinaryOp = 0xE0
	opVPavgw     simdBinaryOp = 0xE3
	opVPcmpeqb   simdBinaryOp = 0x74
	opVPcmpeqd   simdBinaryOp = 0x76
	opVPcmpeqq   simdBinaryOp = 0x29 | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPcmpeqw   simdBinaryOp = 0x75
	opVPcmpgtb   simdBinaryOp = 0x64
	opVPcmpgtd   simdBinaryOp = 0x66
	opVPcmpgtq   simdBinaryOp = 0x37 | simdBinaryOp(shared.AMD64SSE42)<<12
	opVPcmpgtw   simdBinaryOp = 0x65
	opVPhaddd    simdBinaryOp = 0x02 | simdBinaryOp(shared.AMD64SSSE3)<<12
	opVPmaddubsw simdBinaryOp = 0x04 | simdBinaryOp(shared.AMD64SSSE3)<<12
	opVPmaddwd   simdBinaryOp = 0xF5
	opVPmaxsb    simdBinaryOp = 0x3C | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPmaxsd    simdBinaryOp = 0x3D | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPmaxsw    simdBinaryOp = 0xEE
	opVPmaxub    simdBinaryOp = 0xDE
	opVPmaxud    simdBinaryOp = 0x3F | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPmaxuw    simdBinaryOp = 0x3E | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPminsb    simdBinaryOp = 0x38 | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPminsd    simdBinaryOp = 0x39 | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPminsw    simdBinaryOp = 0xEA
	opVPminub    simdBinaryOp = 0xDA
	opVPminud    simdBinaryOp = 0x3B | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPminuw    simdBinaryOp = 0x3A | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPmuldq    simdBinaryOp = 0x28 | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPmulhrsw  simdBinaryOp = 0x0B | simdBinaryOp(shared.AMD64SSSE3)<<12
	opVPmulld    simdBinaryOp = 0x40 | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPmullw    simdBinaryOp = 0xD5
	opVPmuludq   simdBinaryOp = 0xF4
	opVPor       simdBinaryOp = 0xEB
	opVPpackssdw simdBinaryOp = 0x6B
	opVPpacksswb simdBinaryOp = 0x63
	opVPpackusdw simdBinaryOp = 0x2B | simdBinaryOp(shared.AMD64SSE41)<<12
	opVPpackuswb simdBinaryOp = 0x67
	opVPshufb    simdBinaryOp = 0x00 | simdBinaryOp(shared.AMD64SSSE3)<<12
	opVPslld     simdBinaryOp = 0xF2
	opVPsllq     simdBinaryOp = 0xF3
	opVPsllw     simdBinaryOp = 0xF1
	opVPsrad     simdBinaryOp = 0xE2
	opVPsraw     simdBinaryOp = 0xE1
	opVPsrld     simdBinaryOp = 0xD2
	opVPsrlq     simdBinaryOp = 0xD3
	opVPsrlw     simdBinaryOp = 0xD1
	opVPsubb     simdBinaryOp = 0xF8
	opVPsubd     simdBinaryOp = 0xFA
	opVPsubq     simdBinaryOp = 0xFB
	opVPsubsb    simdBinaryOp = 0xE8
	opVPsubsw    simdBinaryOp = 0xE9
	opVPsubusb   simdBinaryOp = 0xD8
	opVPsubusw   simdBinaryOp = 0xD9
	opVPsubw     simdBinaryOp = 0xF9
	opVPunpckhbw simdBinaryOp = 0x68
	opVPunpckhdq simdBinaryOp = 0x6A
	opVPunpckhwd simdBinaryOp = 0x69
	opVPunpcklbw simdBinaryOp = 0x60
	opVPunpckldq simdBinaryOp = 0x62
	opVPunpcklwd simdBinaryOp = 0x61
	opVPxor      simdBinaryOp = 0xEF
)

const (
	opVMovmskpd  simdUnaryOp = 0x50 | 1<<8
	opVMovmskps  simdUnaryOp = 0x50
	opVPabsb     simdUnaryOp = 0x1c | 1<<8 | simdUnaryOp(shared.AMD64SSSE3)<<12
	opVPabsd     simdUnaryOp = 0x1e | 1<<8 | simdUnaryOp(shared.AMD64SSSE3)<<12
	opVPabsw     simdUnaryOp = 0x1d | 1<<8 | simdUnaryOp(shared.AMD64SSSE3)<<12
	opVPmovmskb  simdUnaryOp = 0xd7 | 1<<8
	opVcvtdq2pd  simdUnaryOp = 0xe6 | 2<<8
	opVcvtdq2ps  simdUnaryOp = 0x5b
	opVcvtpd2ps  simdUnaryOp = 0x5a | 1<<8
	opVcvtps2pd  simdUnaryOp = 0x5a
	opVcvttpd2dq simdUnaryOp = 0xe6 | 1<<8
	opVcvttps2dq simdUnaryOp = 0x5b | 2<<8
)

const (
	opVPsllwImm simdShiftImmediate = 0x71 | 6<<8
	opVPsrlwImm simdShiftImmediate = 0x71 | 2<<8
	opVPsrawImm simdShiftImmediate = 0x71 | 4<<8
	opVPslldImm simdShiftImmediate = 0x72 | 6<<8
	opVPsrldImm simdShiftImmediate = 0x72 | 2<<8
	opVPsradImm simdShiftImmediate = 0x72 | 4<<8
	opVPsllqImm simdShiftImmediate = 0x73 | 6<<8
	opVPsrlqImm simdShiftImmediate = 0x73 | 2<<8
)

func (f *fn) emitVPtest(a1, a2 Reg) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VPtest(a1, a2)
		return
	}
	if !f.cpuHas(shared.AMD64SSE41) {
		f.simdTestZero(a1, a2)
		return
	}
	f.a.SseMapRR(0x66, 0x38, 0x17, a1, a2)
}

func (f *fn) emitVFPackedAdd(dst, s1, s2 Reg, f64 bool) {
	simdFloatBinary(0x58, f64, false).emit(f, dst, s1, s2)
}

func (f *fn) emitVFPackedSub(dst, s1, s2 Reg, f64 bool) {
	simdFloatBinary(0x5c, f64, false).emit(f, dst, s1, s2)
}

func (f *fn) emitVFPackedMul(dst, s1, s2 Reg, f64 bool) {
	simdFloatBinary(0x59, f64, false).emit(f, dst, s1, s2)
}

// simdBinaryOp is a compile-time instruction descriptor. Replacing bound
// method values keeps TinyGo from retaining a dispatch arm for every opcode.
// It is never stored in generated code or used during guest invocation.
// Bits 0..7 hold the opcode, 8..11 describe float/imm/swap forms, 12..14
// hold optional SSSE3/SSE4.x requirements, and 16..23 hold an immediate.
type simdBinaryOp uint32

const (
	simdOptionalFeatures simdBinaryOp = simdBinaryOp(shared.AMD64SSSE3|shared.AMD64SSE41|shared.AMD64SSE42) << 12
	simdFloat64          simdBinaryOp = 1 << 8
	simdFloat            simdBinaryOp = 1 << 9
	simdHasImm           simdBinaryOp = 1 << 10
	simdSwap             simdBinaryOp = 1 << 11
)

func simdFloatBinary(op byte, f64, swap bool) simdBinaryOp {
	out := simdBinaryOp(op) | simdFloat
	if f64 {
		out |= simdFloat64
	}
	if swap {
		out |= simdSwap
	}
	return out
}

//go:noinline
func (op simdBinaryOp) emit(f *fn, dst, left, right Reg) {
	if op&simdSwap != 0 {
		left, right = right, left
	}
	f64 := op&simdFloat64 != 0
	if op&simdHasImm != 0 {
		if byte(op) == 0xc2 {
			f.emitVFCmpPacked(dst, left, right, f64, byte(op>>16))
		} else {
			f.emitVShufps(dst, left, right, byte(op>>16))
		}
		return
	}
	feature := shared.AMD64Features(op>>12) & (shared.AMD64SSSE3 | shared.AMD64SSE41 | shared.AMD64SSE42)
	opcodeMap := byte(0)
	if feature != 0 {
		opcodeMap = 0x38
	}
	pp := byte(1)
	if op&simdFloat != 0 && !f64 {
		pp = 0
	}
	if f.cpuHas(shared.AMD64AVX | feature) {
		f.a.VexMapRRR(opcodeMap, pp, byte(op), dst, left, right)
		return
	}
	if feature != 0 && !f.cpuHas(feature) {
		f.simdFallback(byte(op), dst, left, right)
		return
	}
	f.legacySIMDBinary(op, dst, left, right)
}

// simdShiftImmediate holds the x86 opcode and ModRM extension.
type simdShiftImmediate uint16

//go:noinline
func (op simdShiftImmediate) emit(f *fn, dst, src Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VexShiftImm(byte(op), byte(op>>8), dst, src, imm)
		return
	}
	if dst != src {
		f.mov128(dst, src)
	}
	f.a.SseMapRRI(0x66, 0, byte(op), Reg(op>>8), dst, imm)
}

// simdUnaryOp holds an opcode, the VEX pp field, and optional feature bits.
type simdUnaryOp uint32

//go:noinline
func (op simdUnaryOp) emit(f *fn, dst, src Reg) {
	feature := shared.AMD64Features(op >> 12)
	opcodeMap := byte(0)
	if feature != 0 {
		opcodeMap = 0x38
	}
	pp := byte(op>>8) & 3
	if f.cpuHas(shared.AMD64AVX | feature) {
		f.a.VexMapRR(opcodeMap, pp, byte(op), dst, src)
		return
	}
	if feature != 0 && !f.cpuHas(feature) {
		f.simdFallback(byte(op), dst, src, src)
		return
	}
	prefix := byte(0)
	switch pp {
	case 1:
		prefix = 0x66
	case 2:
		prefix = 0xf3
	case 3:
		prefix = 0xf2
	}
	f.a.SseMapRR(prefix, opcodeMap, byte(op), dst, src)
}

func (f *fn) emitPinsrLane(dst, src Reg, lane byte, width int) {
	if f.cpuHas(shared.AMD64SSE41) {
		switch width {
		case 1:
			f.a.Pinsrb(dst, src, lane)
		case 4:
			f.a.Pinsrd(dst, src, lane)
		case 8:
			f.a.Pinsrq(dst, src, lane)
		}
		return
	}
	f.insertSIMDLane(dst, src, lane, width)
}
func (f *fn) emitPextrLane(dst, src Reg, lane byte, width int) {
	if f.cpuHas(shared.AMD64SSE41) {
		switch width {
		case 1:
			f.a.Pextrb(dst, src, lane)
		case 4:
			f.a.Pextrd(dst, src, lane)
		case 8:
			f.a.Pextrq(dst, src, lane)
		}
		return
	}
	f.extractSIMDLane(dst, src, lane, width)
}
