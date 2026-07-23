package railshot

import "github.com/wago-org/wago/src/core/compiler/machinecode"

type CustomInstructionOp uint8

const (
	CustomInstructionInput CustomInstructionOp = iota
	CustomInstructionConst
	CustomInstructionAdd
	CustomInstructionSub
	CustomInstructionMul
	CustomInstructionAnd
	CustomInstructionOr
	CustomInstructionXor
	CustomInstructionNot
	CustomInstructionShl
	CustomInstructionShrU
	CustomInstructionShrS
	CustomInstructionEq
	CustomInstructionNe
	CustomInstructionLtU
	CustomInstructionLtS
	CustomInstructionLeU
	CustomInstructionLeS
	CustomInstructionGtU
	CustomInstructionGtS
	CustomInstructionGeU
	CustomInstructionGeS
	CustomInstructionIsZero
	CustomInstructionSelect
)

type CustomInstructionNode struct {
	Op      CustomInstructionOp
	Width   int32
	A, B, C int
	Input   int
	Const   uint32
}

// CustomSIMDInstruction describes a pointer-based, architecture-neutral wide
// SIMD operation. The physical Wasm parameters are destination first followed
// by Arity input pointers. Backends choose their native vector width.
type CustomSIMDInstruction struct {
	Width     uint16
	Subopcode uint32
	Arity     uint8
}

type CustomInstruction struct {
	Nodes           []CustomInstructionNode
	Output          int
	StackCompatible bool
	AMD64           *machinecode.AMD64Lowering
	SIMD            *CustomSIMDInstruction
	InputWidths     []int32
	ResultWidth     int32
}
