package ir

import "github.com/wago-org/wago/src/core/compiler/wasm"

type Op uint16

const (
	OpInvalid Op = iota
	OpConst
	OpIUnary
	OpIBinary
	OpICmp
	OpITest
	OpFUnary
	OpFBinary
	OpFCmp
	OpConvert
	OpReinterpret
	OpSelect
	OpLoad
	OpStore
	OpMemorySize
	OpMemoryGrow
	OpMemoryCopy
	OpMemoryFill
	OpGlobalGet
	OpGlobalSet
	OpCall
	OpCallImport
	OpCallIndirect
	OpLocalGet
	OpLocalSet
	OpLocalTee
)

// opNames is the single source of truth for IR opcode names used by printing
// and verifier diagnostics. Keeping it next to the enum avoids name drift as
// codegen and optimization passes start matching on Op values.
var opNames = [...]string{
	OpInvalid:      "invalid",
	OpConst:        "const",
	OpIUnary:       "iunary",
	OpIBinary:      "ibinary",
	OpICmp:         "icmp",
	OpITest:        "itest",
	OpFUnary:       "funary",
	OpFBinary:      "fbinary",
	OpFCmp:         "fcmp",
	OpConvert:      "convert",
	OpReinterpret:  "reinterpret",
	OpSelect:       "select",
	OpLoad:         "load",
	OpStore:        "store",
	OpMemorySize:   "memory.size",
	OpMemoryGrow:   "memory.grow",
	OpMemoryCopy:   "memory.copy",
	OpMemoryFill:   "memory.fill",
	OpGlobalGet:    "global.get",
	OpGlobalSet:    "global.set",
	OpCall:         "call",
	OpCallImport:   "call_import",
	OpCallIndirect: "call_indirect",
	OpLocalGet:     "local.get",
	OpLocalSet:     "local.set",
	OpLocalTee:     "local.tee",
}

func opName(op Op) string {
	if int(op) >= 0 && int(op) < len(opNames) && opNames[op] != "" {
		return opNames[op]
	}
	return "invalid"
}

type EffectFlags uint16

const (
	EffectNone    EffectFlags = 0
	EffectCanTrap EffectFlags = 1 << iota
	EffectReadMem
	EffectWriteMem
	EffectReadGlobal
	EffectWriteGlobal
	EffectReadTable
	EffectWriteTable
	EffectCall
	EffectHost
	EffectReadLocal
	EffectWriteLocal
)

type IUnaryOp uint8

const (
	IUnClz IUnaryOp = iota + 1
	IUnCtz
	IUnPopcnt
	IUnExtend8S
	IUnExtend16S
	IUnExtend32S
)

type IBinaryOp uint8

const (
	IBinAdd IBinaryOp = iota + 1
	IBinSub
	IBinMul
	IBinDivS
	IBinDivU
	IBinRemS
	IBinRemU
	IBinAnd
	IBinOr
	IBinXor
	IBinShl
	IBinShrS
	IBinShrU
	IBinRotl
	IBinRotr
)

type ICmpOp uint8

const (
	ICmpEq ICmpOp = iota + 1
	ICmpNe
	ICmpLtS
	ICmpLtU
	ICmpGtS
	ICmpGtU
	ICmpLeS
	ICmpLeU
	ICmpGeS
	ICmpGeU
)

type ITestOp uint8

const (
	ITestEqz ITestOp = iota + 1
)

type FUnaryOp uint8

const (
	FUnAbs FUnaryOp = iota + 1
	FUnNeg
	FUnCeil
	FUnFloor
	FUnTrunc
	FUnNearest
	FUnSqrt
)

type FBinaryOp uint8

const (
	FBinAdd FBinaryOp = iota + 1
	FBinSub
	FBinMul
	FBinDiv
	FBinMin
	FBinMax
	FBinCopySign
)

type FCmpOp uint8

const (
	FCmpEq FCmpOp = iota + 1
	FCmpNe
	FCmpLt
	FCmpGt
	FCmpLe
	FCmpGe
)

type ConvertOp uint8

const (
	ConvWrapI64ToI32 ConvertOp = iota + 1
	ConvTruncFToIS
	ConvTruncFToIU
	ConvExtendI32S
	ConvExtendI32U
	ConvConvertIToFS
	ConvConvertIToFU
	ConvDemoteF64ToF32
	ConvPromoteF32ToF64
	ConvTruncSatFToIS
	ConvTruncSatFToIU
)

type ReinterpretOp uint8

const (
	ReinterpF32ToI32 ReinterpretOp = iota + 1
	ReinterpF64ToI64
	ReinterpI32ToF32
	ReinterpI64ToF64
)

type MemOp uint8

const (
	MemI32 MemOp = iota + 1
	MemI64
	MemF32
	MemF64
	MemI32Load8S
	MemI32Load8U
	MemI32Load16S
	MemI32Load16U
	MemI64Load8S
	MemI64Load8U
	MemI64Load16S
	MemI64Load16U
	MemI64Load32S
	MemI64Load32U
	MemI32Store8
	MemI32Store16
	MemI64Store8
	MemI64Store16
	MemI64Store32
)

func packKindType(kind uint8, t wasm.ValType) uint64 { return uint64(kind) | uint64(t)<<8 }
func auxKind(aux uint64) uint8                       { return uint8(aux) }
func auxType(aux uint64) wasm.ValType                { return wasm.ValType(byte(aux >> 8)) }

func packMem(kind MemOp, align, memidx, offset uint32) uint64 {
	return uint64(kind) | uint64(align)<<8 | uint64(memidx)<<16 | uint64(offset)<<32
}
func memKind(aux uint64) MemOp    { return MemOp(byte(aux)) }
func memAlign(aux uint64) uint32  { return uint32((aux >> 8) & 0xff) }
func memIndex(aux uint64) uint32  { return uint32((aux >> 16) & 0xffff) }
func memOffset(aux uint64) uint32 { return uint32(aux >> 32) }

func packCallIndirect(typeIdx, tableIdx uint32) uint64 { return uint64(typeIdx) | uint64(tableIdx)<<32 }
func callIndirectType(aux uint64) uint32               { return uint32(aux) }
func callIndirectTable(aux uint64) uint32              { return uint32(aux >> 32) }
