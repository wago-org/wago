package railshot

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreplugins "github.com/wago-org/wago/src/core/plugins"
)

// Compatibility aliases keep Railshot's existing integration surface stable
// while core/plugins owns the canonical instruction model.
type CustomInstructionOp = coreplugins.InstructionOp
type CustomInstructionNode = coreplugins.InstructionNode
type CustomSIMDInstruction = coreplugins.SIMDInstruction
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

// ConstantMemoryRangeInMinimum reports whether address..address+size is present
// from instantiation onward. Linear memory never shrinks, so backends may omit a
// runtime bounds check when this proof succeeds.
func ConstantMemoryRangeInMinimum(m *wasm.Module, address, size uint32) bool {
	var pages uint64
	found := false
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternMem {
			pages, found = m.Imports[i].Type.Mem.Limits.Min, true
			break
		}
	}
	if !found && len(m.Memories) != 0 {
		pages, found = m.Memories[0].Limits.Min, true
	}
	if !found {
		return false
	}
	const pageBytes = uint64(65536)
	if pages > ^uint64(0)/pageBytes {
		return true // every i32-addressed range fits in this memory64 minimum
	}
	bytes := pages * pageBytes
	return uint64(size) <= bytes && uint64(address) <= bytes-uint64(size)
}
