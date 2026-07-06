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
	TouchesMemory  bool
	UsesBulkMemory bool
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
	*imm = InstructionImmediate{}
	ir := &reader{data: r.data, pos: r.pos}
	_, err := classifyExprOpAfterOpcode(ir, op, imm)
	r.pos = ir.pos
	return err
}

// SkipInstructionImmediate consumes the immediates for op from r. The opcode
// byte itself must already have been consumed. It validates immediate encodings
// and skips vector immediates without allocating decoded Instruction payloads.
// Structural opcodes (block/loop/if/else/end/try_table) are accepted and only
// their inline immediates are consumed.
func SkipInstructionImmediate(r *Reader, op byte) error {
	var scratch InstructionImmediate
	return ClassifyInstructionImmediateInto(r, op, &scratch)
}
