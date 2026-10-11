//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// X17 normally belongs to encoder address/constant-store helpers. A persistent
// memory-size cache needs a successful-path proof, not merely absence of calls.
// This bounded scalar subset has no such helper: stores, displaced loads,
// globals, SIMD and custom instructions retain the ordinary reserved register.
func (f *fn) leafScratchMemorySafe(body []byte) bool {
	if len(body) == 0 || len(body) > 4096 || len(f.customInstructions) != 0 || f.memoryAddr64(0) || f.threadedMemory0 || f.tracksGCFrameRoots() {
		return false
	}
	for _, typ := range f.localType {
		if typ != mtI32 && typ != mtI64 {
			return false
		}
	}
	r := wasm.NewReader(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		load := op == 0x28 || op == 0x29 || op >= 0x2c && op <= 0x35
		switch {
		case load:
		case op == 0x01 || op == 0x0b || op == 0x0f || op == 0x1a || op == 0x1b || op == 0x1c ||
			op >= 0x20 && op <= 0x22 || op == 0x41 || op == 0x42 || op >= 0x45 && op <= 0x5a ||
			op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad || op >= 0xc0 && op <= 0xc4:
		default:
			return false
		}
		if err := f.classifier.ClassifyInto(r, op, &imm); err != nil {
			return false
		}
		if load && (imm.MemOffset != 0 || imm.HasMemIndex && imm.MemIndex != 0) {
			return false
		}
	}
	return true
}
