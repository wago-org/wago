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

type CustomInstruction struct {
	Nodes           []CustomInstructionNode
	Output          int
	StackCompatible bool
	AMD64           *machinecode.AMD64Lowering
	InputWidths     []int32
	ResultWidth     int32
}
