package railshot

import (
	"github.com/wago-org/wago/src/core/compiler/machinecode"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

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
