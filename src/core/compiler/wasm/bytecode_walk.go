package wasm

// SkipInstructionImmediate consumes the immediates for op from r. The opcode
// byte itself must already have been consumed. It validates immediate encodings
// and skips vector immediates without allocating decoded Instruction payloads.
// Structural opcodes (block/loop/if/else/end/try_table) are accepted and only
// their inline immediates are consumed.
func SkipInstructionImmediate(r *Reader, op byte) error {
	ir := &reader{data: r.data, pos: r.pos}
	_, err := skipExprOpAfterOpcode(ir, op)
	r.pos = ir.pos
	return err
}
