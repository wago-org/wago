package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// IsStraightLineBlock proves that a small block has only ordinary numeric,
// local/global and memory operations followed by its end. No instruction can
// branch, call, introduce another control frame, or make fallthrough unreachable.
// Keeping the control frame is still necessary for its stack-height contract;
// only entry/exit reconciliation can be omitted. The original reader is unchanged.
func IsStraightLineBlock(source *wasm.Reader, classifier wasm.ModuleInstructionClassifier) bool {
	r := *source
	start := r.Offset()
	var imm wasm.InstructionImmediate
	for n := 0; n < 32 && r.Offset()-start < 128; n++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x0b {
			return true
		}
		if !(op == 0x01 || op >= 0x1a && op <= 0x1c || op >= 0x20 && op <= 0x24 || op >= 0x28 && op <= 0x3f || op >= 0x41 && op <= 0xc4 || op == 0xfd) {
			return false
		}
		if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
			return false
		}
	}
	return false
}
