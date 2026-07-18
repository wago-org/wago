package embedded32

import "encoding/binary"

// Helper table byte offsets. A board runtime publishes Thumb-bit-adjusted or
// RV32 code pointers in these slots before generated code executes.
const (
	HelperF64Offset  = 0
	HelperSIMDOffset = 4
	HelperI64Offset  = 8
	HelperTableBytes = 12
)

// Stable F64Frame byte offsets used by generated code. F64Frame intentionally
// contains an explicit three-byte pad after Op so these are identical on 32- and
// 64-bit Go/TinyGo hosts.
type DataSegmentABI struct {
	Base    uint32
	Length  uint32
	Dropped uint32
}

const (
	DataSegmentBaseOffset    = 0
	DataSegmentLengthOffset  = 4
	DataSegmentDroppedOffset = 8
	DataSegmentABIBytes      = 12

	I64FrameOpOffset     = 0
	I64FrameALoOffset    = 4
	I64FrameAHiOffset    = 8
	I64FrameBLoOffset    = 12
	I64FrameBHiOffset    = 16
	I64FrameOutLoOffset  = 20
	I64FrameOutHiOffset  = 24
	I64FrameI32OutOffset = 28
	I64FrameTrapOffset   = 32
	I64FrameBytes        = 36

	F64FrameOpOffset    = 0
	F64FrameALoOffset   = 4
	F64FrameAHiOffset   = 8
	F64FrameBLoOffset   = 12
	F64FrameBHiOffset   = 16
	F64FrameOutLoOffset = 20
	F64FrameOutHiOffset = 24
	F64FrameTrapOffset  = 28
	F64FrameBytes       = 32
)

// SIMDABIFrame is the fixed 32-bit-slot representation consumed by generated
// code. RunSIMDABI converts it to the byte-oriented semantic frame without
// allocation. Its layout is independent of host pointer and uint64 alignment.
type SIMDABIFrame struct {
	Op                       uint32
	ScalarLo, ScalarHi       uint32
	A, B, C, Immediate, Out  [4]uint32
	ScalarOutLo, ScalarOutHi uint32
	MemoryBase, MemoryLen    uint32
	Address, Lane            uint32
	Trap                     Trap
}

const (
	SIMDFrameOpOffset         = 0
	SIMDFrameScalarLoOffset   = 4
	SIMDFrameScalarHiOffset   = 8
	SIMDFrameAOffset          = 12
	SIMDFrameBOffset          = 28
	SIMDFrameCOffset          = 44
	SIMDFrameImmediateOffset  = 60
	SIMDFrameOutOffset        = 76
	SIMDFrameScalarOutOffset  = 92
	SIMDFrameMemoryBaseOffset = 100
	SIMDFrameMemoryLenOffset  = 104
	SIMDFrameAddressOffset    = 108
	SIMDFrameLaneOffset       = 112
	SIMDFrameTrapOffset       = 116
	SIMDFrameBytes            = 120
)

func wordsToV128(words [4]uint32) (v V128) {
	for i, x := range words {
		binary.LittleEndian.PutUint32(v[i*4:], x)
	}
	return v
}
func v128ToWords(v V128) (words [4]uint32) {
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(v[i*4:])
	}
	return words
}

// RunSIMDABI is the firmware-facing helper entry with a stable 32-bit layout.
//
//export wago_embedded32_simd_abi
func RunSIMDABI(abi *SIMDABIFrame) {
	memory := memoryFromABI(abi.MemoryBase, abi.MemoryLen)
	f := SIMDFrame{
		Op:     abi.Op,
		Scalar: uint64(abi.ScalarLo) | uint64(abi.ScalarHi)<<32,
		A:      wordsToV128(abi.A), B: wordsToV128(abi.B), C: wordsToV128(abi.C),
		Immediate: wordsToV128(abi.Immediate), Memory: memory, Address: abi.Address, Lane: abi.Lane,
	}
	RunSIMD(&f)
	abi.Out = v128ToWords(f.Out)
	abi.ScalarOutLo, abi.ScalarOutHi = uint32(f.ScalarOut), uint32(f.ScalarOut>>32)
	abi.Trap = f.Trap
}
