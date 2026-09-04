package wasm

// SIMDEffectClass describes the validator-authoritative operand shape of one
// SIMD instruction. Memory address width remains module-dependent; consumers
// combine this class with their selected memory's address type.
type SIMDEffectClass uint8

const (
	SIMDEffectInvalid SIMDEffectClass = iota
	SIMDEffectLoad
	SIMDEffectStore
	SIMDEffectLoadLane
	SIMDEffectStoreLane
	SIMDEffectSplat
	SIMDEffectExtract
	SIMDEffectReplace
	SIMDEffectShift
	SIMDEffectUnary
	SIMDEffectBinary
	SIMDEffectTernary
	SIMDEffectReduceI32
	SIMDEffectBitselect
	SIMDEffectConst
)

// SIMDInstructionDescriptor is the cold-path decoded form used by compiler
// backends. It avoids adding vector payloads to InstructionImmediate, which is
// intentionally compact for scalar bytecode walks.
type SIMDInstructionDescriptor struct {
	Kind         InstrKind
	Class        SIMDEffectClass
	Scalar       ValType
	Subopcode    uint32
	MemArg       MemArg
	Lane         LaneIdx
	LaneLimit    LaneIdx
	NaturalAlign uint8
	Bytes        [16]byte
}

// DecodeSIMDInstruction consumes one 0xFD instruction after the prefix byte has
// already been read. The module supplies per-memory offset widths.
func DecodeSIMDInstruction(r *Reader, m *Module, multiMemory bool) (SIMDInstructionDescriptor, error) {
	start := r.pos
	subopcode, err := r.U32()
	if err != nil {
		return SIMDInstructionDescriptor{}, err
	}
	r.pos = start
	widths := moduleMemargWidths(m)
	ir := reader{data: r.data, pos: r.pos}
	var ext instrExt
	instruction, err := decodeFDWithMemargWidthsInto(&ir, widths, &ext)
	r.pos = ir.pos
	if err != nil {
		return SIMDInstructionDescriptor{}, err
	}
	effect := simdEffects[instruction.Kind]
	class := publicSIMDEffectClass(effect.cat)
	if class == SIMDEffectInvalid {
		return SIMDInstructionDescriptor{}, &DecodeError{Code: ErrInvalidInstruction, Offset: start}
	}
	if effect.laneLimit != 0 && instruction.Lane >= effect.laneLimit {
		return SIMDInstructionDescriptor{}, &DecodeError{Code: ErrInvalidInstruction, Offset: start}
	}
	d := SIMDInstructionDescriptor{
		Kind: instruction.Kind, Class: class, Scalar: effect.scalar.valType(),
		Subopcode: subopcode, MemArg: instruction.MemArg(), Lane: instruction.Lane,
		LaneLimit: effect.laneLimit, NaturalAlign: effect.align,
	}
	if d.MemArg.Mem != nil && !multiMemory {
		return SIMDInstructionDescriptor{}, &DecodeError{Code: ErrInvalidInstruction, Offset: start}
	}
	lanes := instruction.Lanes()
	for i := range d.Bytes {
		d.Bytes[i] = byte(lanes[i])
	}
	return d, nil
}

func publicSIMDEffectClass(class simdEffectCat) SIMDEffectClass {
	switch class {
	case simdEffLoad:
		return SIMDEffectLoad
	case simdEffStore:
		return SIMDEffectStore
	case simdEffMemLoadLane:
		return SIMDEffectLoadLane
	case simdEffMemStoreLane:
		return SIMDEffectStoreLane
	case simdEffSplat:
		return SIMDEffectSplat
	case simdEffExtract:
		return SIMDEffectExtract
	case simdEffReplace:
		return SIMDEffectReplace
	case simdEffShift:
		return SIMDEffectShift
	case simdEffUnary:
		return SIMDEffectUnary
	case simdEffBinary:
		return SIMDEffectBinary
	case simdEffTernary:
		return SIMDEffectTernary
	case simdPopV128PushI32:
		return SIMDEffectReduceI32
	case simdBitselect:
		return SIMDEffectBitselect
	case simdConst:
		return SIMDEffectConst
	default:
		return SIMDEffectInvalid
	}
}
