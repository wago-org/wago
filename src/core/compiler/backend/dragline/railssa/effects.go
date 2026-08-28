package railssa

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type HeapMask uint16

const (
	HeapLinearMemory HeapMask = 1 << iota
	HeapTable
	HeapGlobal
	HeapGCHeader
	HeapGCStruct
	HeapGCArray
	HeapImportState
	HeapRuntimeState
	HeapHostUnknown
)

type EffectFlags uint16

const (
	EffectMayGrow EffectFlags = 1 << iota
	EffectMayAllocate
	EffectMayCollect
	EffectMayReenter
	EffectMayThrow
	EffectMayTrap
	EffectCall
)

type TrapMask uint16

const (
	TrapUnreachable TrapMask = 1 << iota
	TrapMemoryBounds
	TrapIntegerDivideByZero
	TrapIntegerOverflow
	TrapInvalidConversion
	TrapTableBounds
	TrapIndirectNull
	TrapIndirectSignature
	TrapNullReference
	TrapArrayBounds
	TrapCastFailure
)

type ObligationMask uint16

const (
	ObligationTrapOrder ObligationMask = 1 << iota
	ObligationMemoryBounds
	ObligationNonzeroDivisor
	ObligationSignedDivisionRange
	ObligationFiniteConversion
	ObligationTableBounds
	ObligationIndirectTarget
)

// InstructionMetadata keeps effects, traps, source location, and ordering in a
// compact side table so target lowering cannot accidentally discard semantics.
type InstructionMetadata struct {
	Offset      uint32
	Epoch       uint32
	Reads       HeapMask
	Writes      HeapMask
	Flags       EffectFlags
	Traps       TrapMask
	Obligations ObligationMask
}

type Metadata struct {
	Instructions []InstructionMetadata
	Epochs       uint32
}

func BuildMetadata(f *StackFunc, reuse *Metadata) (*Metadata, error) {
	if f == nil {
		return nil, fmt.Errorf("railssa: metadata requires a function")
	}
	if reuse == nil {
		reuse = new(Metadata)
	}
	instructions := resizeClear(reuse.Instructions, len(f.Instrs))
	*reuse = Metadata{Instructions: instructions}
	for index, instruction := range f.Instrs {
		meta := classifyInstruction(f, uint32(index), instruction)
		meta.Offset = instruction.Offset
		reuse.Instructions[index] = meta
	}
	rebuildMetadataEpochs(reuse)
	if err := VerifyMetadata(f, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func rebuildMetadataEpochs(metadata *Metadata) {
	epoch := uint32(0)
	for index := range metadata.Instructions {
		meta := &metadata.Instructions[index]
		meta.Epoch = epoch
		if meta.Writes != 0 || meta.Flags&(EffectMayGrow|EffectMayAllocate|EffectMayCollect|EffectMayReenter|EffectMayThrow|EffectMayTrap) != 0 {
			epoch++
		}
	}
	metadata.Epochs = epoch + 1
}

// RefineHostEffects replaces conservative imported-call metadata only when an
// explicit trusted contract exists. It runs before simplification, pressure
// shaping, and scheduling so every consumer sees the same refined semantics.
func RefineHostEffects(f *StackFunc, metadata *Metadata, host []HostEffectContract) error {
	if err := VerifyMetadata(f, metadata); err != nil {
		return err
	}
	if len(host) != 0 && len(host) != int(f.ImportedFuncs) {
		return fmt.Errorf("railssa: host effect count %d, want %d", len(host), f.ImportedFuncs)
	}
	if len(host) == 0 {
		return nil
	}
	for index, instruction := range f.Instrs {
		if instruction.Kind != wasm.InstrCall || instruction.U32() >= f.ImportedFuncs {
			continue
		}
		contract := host[instruction.U32()]
		if !contract.Declared {
			continue
		}
		metadata.Instructions[index].Reads = contract.Reads
		metadata.Instructions[index].Writes = contract.Writes
		metadata.Instructions[index].Flags = contract.Flags | EffectCall
	}
	rebuildMetadataEpochs(metadata)
	return VerifyRefinedHostEffects(f, metadata, host)
}

func VerifyRefinedHostEffects(f *StackFunc, metadata *Metadata, host []HostEffectContract) error {
	if err := VerifyMetadata(f, metadata); err != nil {
		return err
	}
	for index, instruction := range f.Instrs {
		if instruction.Kind != wasm.InstrCall || instruction.U32() >= f.ImportedFuncs {
			continue
		}
		contract := host[instruction.U32()]
		if !contract.Declared {
			continue
		}
		meta := metadata.Instructions[index]
		if meta.Reads != contract.Reads || meta.Writes != contract.Writes || meta.Flags != contract.Flags|EffectCall {
			return fmt.Errorf("railssa: imported call %d does not match its host effect contract", index)
		}
	}
	return nil
}

func classifyInstruction(f *StackFunc, source uint32, instruction StackInstr) InstructionMetadata {
	kind := instruction.Kind
	var meta InstructionMetadata
	switch {
	case kind == wasm.InstrUnreachable:
		meta.Traps = TrapUnreachable
		meta.Obligations = ObligationTrapOrder
	case kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Load32U:
		meta.Reads = HeapLinearMemory
		meta.Traps = TrapMemoryBounds
		meta.Obligations = ObligationTrapOrder | ObligationMemoryBounds
	case kind >= wasm.InstrI32Store && kind <= wasm.InstrI64Store32:
		meta.Writes = HeapLinearMemory
		meta.Traps = TrapMemoryBounds
		meta.Obligations = ObligationTrapOrder | ObligationMemoryBounds
	case wasm.IsSIMDValidationInstructionKind(kind):
		if descriptor, ok := f.SIMDImmediateAt(source); ok {
			switch descriptor.Class {
			case wasm.SIMDEffectLoad, wasm.SIMDEffectLoadLane:
				meta.Reads = HeapLinearMemory
				meta.Traps = TrapMemoryBounds
				meta.Obligations = ObligationTrapOrder | ObligationMemoryBounds
			case wasm.SIMDEffectStore, wasm.SIMDEffectStoreLane:
				meta.Writes = HeapLinearMemory
				meta.Traps = TrapMemoryBounds
				meta.Obligations = ObligationTrapOrder | ObligationMemoryBounds
			}
		}
	case kind == wasm.InstrMemorySize:
		meta.Reads = HeapLinearMemory | HeapRuntimeState
	case kind == wasm.InstrMemoryGrow:
		meta.Reads = HeapLinearMemory | HeapRuntimeState
		meta.Writes = HeapLinearMemory | HeapRuntimeState
		meta.Flags = EffectMayGrow
	case kind == wasm.InstrMemoryCopy:
		meta.Reads = HeapLinearMemory
		meta.Writes = HeapLinearMemory
		meta.Traps = TrapMemoryBounds
		meta.Obligations = ObligationTrapOrder | ObligationMemoryBounds
	case kind == wasm.InstrMemoryFill:
		meta.Writes = HeapLinearMemory
		meta.Traps = TrapMemoryBounds
		meta.Obligations = ObligationTrapOrder | ObligationMemoryBounds
	case kind == wasm.InstrDataDrop:
		meta.Writes = HeapRuntimeState
	case kind == wasm.InstrElemDrop:
		meta.Writes = HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
	case kind == wasm.InstrGlobalGet:
		meta.Reads = HeapGlobal
	case kind == wasm.InstrGlobalSet:
		meta.Writes = HeapGlobal
	case kind == wasm.InstrCall:
		meta.Flags = EffectCall
		if instruction.U32() < f.ImportedFuncs {
			meta.Reads = HeapImportState | HeapRuntimeState | HeapHostUnknown
			meta.Writes = meta.Reads
			meta.Flags |= EffectMayAllocate | EffectMayCollect | EffectMayReenter | EffectMayThrow
		} else {
			// Until compact callee summaries are threaded into construction, a
			// local call is conservatively a runtime/heap barrier.
			meta.Reads = HeapLinearMemory | HeapTable | HeapGlobal | HeapRuntimeState | HeapHostUnknown
			meta.Writes = meta.Reads
			meta.Flags |= EffectMayAllocate | EffectMayCollect | EffectMayReenter | EffectMayThrow
		}
	case kind == wasm.InstrCallIndirect:
		meta.Reads = HeapTable | HeapLinearMemory | HeapGlobal | HeapRuntimeState | HeapHostUnknown
		meta.Writes = HeapLinearMemory | HeapGlobal | HeapRuntimeState | HeapHostUnknown
		meta.Flags = EffectCall | EffectMayAllocate | EffectMayCollect | EffectMayReenter | EffectMayThrow
		meta.Traps = TrapTableBounds | TrapIndirectNull | TrapIndirectSignature
		meta.Obligations = ObligationTrapOrder | ObligationTableBounds | ObligationIndirectTarget
	case kind == wasm.InstrRefFunc:
		// The canonical descriptor array belongs to the instance runtime state.
		// It is immutable after instantiation but prevents treating ref.func as a
		// context-free constant or rematerializing it across instance boundaries.
		meta.Reads = HeapRuntimeState
	case kind == wasm.InstrRefAsNonNull:
		meta.Traps = TrapNullReference
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrI31GetS || kind == wasm.InstrI31GetU:
		meta.Traps = TrapNullReference
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrAnyConvertExtern || kind == wasm.InstrExternConvertAny:
		meta.Reads = HeapRuntimeState
		meta.Writes = HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
	case kind == wasm.InstrRefTest:
		meta.Reads = HeapGCHeader | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
	case kind == wasm.InstrBrOnCast || kind == wasm.InstrBrOnCastFail:
		meta.Reads = HeapGCHeader | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
	case kind == wasm.InstrRefCast:
		meta.Reads = HeapGCHeader | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
		meta.Traps = TrapCastFailure
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrStructNew || kind == wasm.InstrStructNewDefault:
		meta.Writes = HeapGCHeader | HeapGCStruct | HeapRuntimeState
		meta.Flags = EffectMayAllocate | EffectMayCollect | EffectMayThrow
	case kind == wasm.InstrArrayNew || kind == wasm.InstrArrayNewDefault || kind == wasm.InstrArrayNewFixed || kind == wasm.InstrArrayNewData || kind == wasm.InstrArrayNewElem:
		meta.Writes = HeapGCHeader | HeapGCArray | HeapRuntimeState
		meta.Flags = EffectMayAllocate | EffectMayCollect | EffectMayThrow
	case kind == wasm.InstrStructGet || kind == wasm.InstrStructGetS || kind == wasm.InstrStructGetU:
		meta.Reads = HeapGCHeader | HeapGCStruct | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
		meta.Traps = TrapNullReference
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrStructSet:
		meta.Reads = HeapGCHeader | HeapRuntimeState
		meta.Writes = HeapGCStruct | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
		meta.Traps = TrapNullReference
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrArrayGet || kind == wasm.InstrArrayGetS || kind == wasm.InstrArrayGetU:
		meta.Reads = HeapGCHeader | HeapGCArray | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
		meta.Traps = TrapNullReference | TrapArrayBounds
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrArraySet:
		meta.Reads = HeapGCHeader | HeapRuntimeState
		meta.Writes = HeapGCArray | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
		meta.Traps = TrapNullReference | TrapArrayBounds
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrArrayFill || kind == wasm.InstrArrayCopy:
		meta.Reads = HeapGCHeader | HeapGCArray | HeapRuntimeState
		meta.Writes = HeapGCArray | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
		meta.Traps = TrapNullReference | TrapArrayBounds
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrArrayInitData || kind == wasm.InstrArrayInitElem:
		meta.Reads = HeapGCHeader | HeapRuntimeState
		meta.Writes = HeapGCArray | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
		meta.Traps = TrapNullReference | TrapArrayBounds
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrArrayLen:
		meta.Reads = HeapGCHeader | HeapGCArray | HeapRuntimeState
		meta.Flags = EffectCall | EffectMayThrow
		meta.Traps = TrapNullReference
		meta.Obligations = ObligationTrapOrder
	case kind == wasm.InstrI32DivS || kind == wasm.InstrI64DivS:
		meta.Traps = TrapIntegerDivideByZero | TrapIntegerOverflow
		meta.Obligations = ObligationTrapOrder | ObligationNonzeroDivisor | ObligationSignedDivisionRange
	case kind == wasm.InstrI32DivU || kind == wasm.InstrI32RemS || kind == wasm.InstrI32RemU ||
		kind == wasm.InstrI64DivU || kind == wasm.InstrI64RemS || kind == wasm.InstrI64RemU:
		meta.Traps = TrapIntegerDivideByZero
		meta.Obligations = ObligationTrapOrder | ObligationNonzeroDivisor
	case kind >= wasm.InstrI32TruncF32S && kind <= wasm.InstrI32TruncF64U ||
		kind >= wasm.InstrI64TruncF32S && kind <= wasm.InstrI64TruncF64U:
		meta.Traps = TrapInvalidConversion
		meta.Obligations = ObligationTrapOrder | ObligationFiniteConversion
	}
	if meta.Traps != 0 {
		meta.Flags |= EffectMayTrap
	}
	return meta
}

func VerifyMetadata(f *StackFunc, metadata *Metadata) error {
	if f == nil || metadata == nil || len(metadata.Instructions) != len(f.Instrs) {
		return fmt.Errorf("railssa: metadata length mismatch")
	}
	lastEpoch := uint32(0)
	for i, meta := range metadata.Instructions {
		if meta.Offset != f.Instrs[i].Offset {
			return fmt.Errorf("railssa: metadata %d source offset %d, want %d", i, meta.Offset, f.Instrs[i].Offset)
		}
		if i != 0 && meta.Offset < metadata.Instructions[i-1].Offset {
			return fmt.Errorf("railssa: metadata source offsets are unordered at %d", i)
		}
		if meta.Epoch < lastEpoch {
			return fmt.Errorf("railssa: metadata epoch regresses at %d", i)
		}
		lastEpoch = meta.Epoch
		if meta.Traps != 0 && (meta.Flags&EffectMayTrap == 0 || meta.Obligations&ObligationTrapOrder == 0) {
			return fmt.Errorf("railssa: trapping instruction %d lacks trap flag or order obligation", i)
		}
		if meta.Traps == 0 && meta.Flags&EffectMayTrap != 0 {
			return fmt.Errorf("railssa: instruction %d has trap flag without trap kind", i)
		}
	}
	if metadata.Epochs == 0 || len(metadata.Instructions) != 0 && metadata.Epochs <= lastEpoch {
		return fmt.Errorf("railssa: invalid epoch count %d", metadata.Epochs)
	}
	return nil
}
