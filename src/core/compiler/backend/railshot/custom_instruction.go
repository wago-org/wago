package railshot

import coreplugins "github.com/wago-org/wago/src/core/plugins"

// Compatibility aliases keep Railshot's existing integration surface stable
// while core/plugins owns the canonical instruction model.
type CustomInstructionOp = coreplugins.InstructionOp
type CustomInstructionNode = coreplugins.InstructionNode
type CustomInstruction = coreplugins.Instruction

const (
	CustomInstructionInput  = coreplugins.InstructionInput
	CustomInstructionConst  = coreplugins.InstructionConst
	CustomInstructionAdd    = coreplugins.InstructionAdd
	CustomInstructionSub    = coreplugins.InstructionSub
	CustomInstructionMul    = coreplugins.InstructionMul
	CustomInstructionAnd    = coreplugins.InstructionAnd
	CustomInstructionOr     = coreplugins.InstructionOr
	CustomInstructionXor    = coreplugins.InstructionXor
	CustomInstructionNot    = coreplugins.InstructionNot
	CustomInstructionShl    = coreplugins.InstructionShl
	CustomInstructionShrU   = coreplugins.InstructionShrU
	CustomInstructionShrS   = coreplugins.InstructionShrS
	CustomInstructionEq     = coreplugins.InstructionEq
	CustomInstructionNe     = coreplugins.InstructionNe
	CustomInstructionLtU    = coreplugins.InstructionLtU
	CustomInstructionLtS    = coreplugins.InstructionLtS
	CustomInstructionLeU    = coreplugins.InstructionLeU
	CustomInstructionLeS    = coreplugins.InstructionLeS
	CustomInstructionGtU    = coreplugins.InstructionGtU
	CustomInstructionGtS    = coreplugins.InstructionGtS
	CustomInstructionGeU    = coreplugins.InstructionGeU
	CustomInstructionGeS    = coreplugins.InstructionGeS
	CustomInstructionIsZero = coreplugins.InstructionIsZero
	CustomInstructionSelect = coreplugins.InstructionSelect
)
