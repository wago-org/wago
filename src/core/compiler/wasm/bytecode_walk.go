package wasm

// InstructionImmediate is the allocation-free classification returned while
// consuming an instruction's encoded immediates.
type InstructionImmediate struct {
	Kind           InstrKind
	Prefix         byte
	Subopcode      uint32
	Index          uint32
	Index2         uint32
	HasMemIndex    bool
	MemIndex       uint32
	MemAlign       uint32
	MemOffset      uint64
	TouchesMemory  bool
	UsesBulkMemory bool
}

// ImmediateFreeInstructionKind reports the decoded kind for an opcode whose
// instruction has no encoded immediate. Bytecode summary walkers use this fast
// path to avoid constructing a reader adapter and running the full classifier
// for the common scalar ALU, comparison, conversion, and stack instructions.
func ImmediateFreeInstructionKind(op byte) (InstrKind, bool) {
	kind := simpleOpcode[op]
	return kind, kind != InstrInvalid
}

// ClassifyInstructionImmediate consumes the immediates for op from r and returns
// cheap metadata needed by bytecode walkers. The opcode byte itself must already
// have been consumed. It validates immediate encodings and skips vector
// immediates without allocating decoded Instruction payloads.
func ClassifyInstructionImmediate(r *Reader, op byte) (InstructionImmediate, error) {
	var imm InstructionImmediate
	err := ClassifyInstructionImmediateInto(r, op, &imm)
	return imm, err
}

// ClassifyInstructionImmediateInto is ClassifyInstructionImmediate with a
// caller-provided out-param, avoiding the return-value copy on hot compile paths.
// It zeroes *imm first, so the buffer may be reused across calls.
func ClassifyInstructionImmediateInto(r *Reader, op byte, imm *InstructionImmediate) error {
	return ClassifyInstructionImmediateIntoWithMemarg64(r, op, imm, false)
}

// ClassifyInstructionImmediateIntoWithMemarg64 is the memory64-aware form used
// by module walkers after validation has established the module's memarg width.
// The width flag affects only memory offsets; instruction classification and all
// malformed-immediate checks remain identical to ClassifyInstructionImmediateInto.
func ClassifyInstructionImmediateIntoWithMemarg64(r *Reader, op byte, imm *InstructionImmediate, memarg64 bool) error {
	return ClassifyInstructionImmediateIntoWithFeatures(r, op, imm, memarg64, true)
}

// ClassifyInstructionImmediateIntoWithFeatures is the staged-feature form used
// by validated module walkers. multiMemory selects indexed memargs and memory
// index grammar for scalar and bulk instructions; memarg64 selects u64 offsets.
func ClassifyInstructionImmediateIntoWithFeatures(r *Reader, op byte, imm *InstructionImmediate, memarg64, multiMemory bool) error {
	return classifyInstructionImmediateIntoWithWidths(r, op, imm, fixedMemargWidths(memarg64), multiMemory)
}

// ModuleInstructionClassifier caches one module's memarg-width classification
// for allocation-free bytecode walks. Construct it once per walk rather than
// rescanning imported memories for every instruction.
type ModuleInstructionClassifier struct {
	widths      memargWidths
	multiMemory bool
}

func NewModuleInstructionClassifier(m *Module, multiMemory bool) ModuleInstructionClassifier {
	return ModuleInstructionClassifier{widths: moduleMemargWidths(m), multiMemory: multiMemory}
}

func (c ModuleInstructionClassifier) ClassifyInto(r *Reader, op byte, imm *InstructionImmediate) error {
	return classifyInstructionImmediateIntoWithWidths(r, op, imm, c.widths, c.multiMemory)
}

// ClassifyInstructionImmediateIntoWithModuleFeatures selects each memarg offset
// width from the memory index encoded by that instruction. It is intended for
// one-shot classification; repeated walks should cache NewModuleInstructionClassifier.
func ClassifyInstructionImmediateIntoWithModuleFeatures(r *Reader, op byte, imm *InstructionImmediate, m *Module, multiMemory bool) error {
	return NewModuleInstructionClassifier(m, multiMemory).ClassifyInto(r, op, imm)
}

func classifyInstructionImmediateIntoWithWidths(r *Reader, op byte, imm *InstructionImmediate, widths memargWidths, multiMemory bool) error {
	*imm = InstructionImmediate{}
	if kind := simpleOpcode[op]; kind != InstrInvalid {
		imm.Kind = kind
		return nil
	}
	// Keep the common scalar immediate forms on the exported Reader. This avoids
	// constructing the internal decoder adapter for the local/global accesses,
	// calls, branches, and constants that dominate summary walks.
	switch op {
	case 0x05, 0x0b: // else, end
		return nil
	case 0x08, 0x0c, 0x0d, 0x10, 0x12, 0x14, 0x15, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0xd2, 0xd5, 0xd6:
		idx, err := r.U32()
		imm.Kind, imm.Index = oneIndexImmediateKind(op), idx
		return err
	case 0x41:
		_, err := r.I32()
		imm.Kind = InstrI32Const
		return err
	case 0x42:
		_, err := r.I64()
		imm.Kind = InstrI64Const
		return err
	case 0x43:
		_, err := r.Bytes(4)
		imm.Kind = InstrF32Const
		return err
	case 0x44:
		_, err := r.Bytes(8)
		imm.Kind = InstrF64Const
		return err
	}
	ir := reader{data: r.data, pos: r.pos}
	_, err := classifyExprOpAfterOpcodeWithWidths(&ir, op, imm, widths, multiMemory)
	r.pos = ir.pos
	return err
}

// SkipInstructionImmediate consumes the immediates for op from r. The opcode
// byte itself must already have been consumed. It validates immediate encodings
// and skips vector immediates without allocating decoded Instruction payloads.
// Structural opcodes (block/loop/if/else/end/try_table) are accepted and only
// their inline immediates are consumed.
func SkipInstructionImmediate(r *Reader, op byte) error {
	return SkipInstructionImmediateWithMemarg64(r, op, false)
}

// SkipInstructionImmediateWithMemarg64 is the memory-width-aware form used by
// backends while traversing validated dead code.
func SkipInstructionImmediateWithMemarg64(r *Reader, op byte, memarg64 bool) error {
	var scratch InstructionImmediate
	return ClassifyInstructionImmediateIntoWithMemarg64(r, op, &scratch, memarg64)
}
