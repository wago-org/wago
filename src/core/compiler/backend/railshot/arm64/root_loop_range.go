//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Recognize a zero-entry positive induction loop with exactly one counter write
// and one canonical conditional backedge. Divisibility makes the progression
// reach its positive limit before any i32 wrap. The inclusive limit remains a
// valid upper bound even after the final update. No trapping check is hoisted.
func (f *fn) proveRootLoopRange(body []byte, zero uint64) (local uint8, maximum uint32) {
	if len(body) > 16<<10 {
		return
	}
	type instruction struct {
		op       byte
		index    uint32
		constant int32
	}
	var recent [8]instruction
	var writes [64]uint16
	count, branches := 0, 0
	r := wasm.ReaderFrom(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return 0, 0
		}
		if !(op == 0x01 || op == 0x1a || op == 0x1b || op == 0x0d || op == 0x0b || op >= 0x20 && op <= 0x22 || op >= 0x28 && op <= 0x3e || op >= 0x41 && op <= 0xc4) {
			return 0, 0
		}
		in := instruction{op: op}
		copy := r
		switch op {
		case 0x41:
			in.constant, err = copy.I32()
		case 0x20, 0x21, 0x22, 0x0d:
			in.index, err = copy.U32()
		}
		if err != nil || f.classifier.ClassifyInto(&r, op, &imm) != nil {
			return 0, 0
		}
		if op == 0x21 || op == 0x22 {
			if int(in.index) >= f.nLocals || in.index >= 64 {
				return 0, 0
			}
			writes[in.index]++
		}
		if op == 0x0d {
			branches++
		}
		if op == 0x0b && r.HasNext() {
			return 0, 0
		}
		recent[count%len(recent)] = in
		count++
	}
	if count < 8 || branches != 1 {
		return
	}
	var tail [8]instruction
	for i := range tail {
		tail[i] = recent[(count-8+i)%8]
	}
	for i, op := range []byte{0x20, 0x41, 0x6a, 0x22, 0x41, 0x47, 0x0d, 0x0b} {
		if tail[i].op != op {
			return 0, 0
		}
	}
	index := tail[0].index
	if index >= 64 || int(index) >= f.nLocals || int(index) < f.nParams || f.localType[index] != mtI32 || zero&(uint64(1)<<index) == 0 || tail[3].index != index || writes[index] != 1 || tail[6].index != 0 {
		return
	}
	stride, limit := tail[1].constant, tail[4].constant
	if stride <= 0 || limit <= 0 || stride > limit || limit%stride != 0 {
		return
	}
	return uint8(index + 1), uint32(limit)
}

// Only the first outermost loop is considered, bounding lookahead to one body
// and one prefix scan per function. A declared local never written before this
// entry is still zero. The matched latch proves a non-wrapping bound.
// Later source reuse is outside this frame's facts.
func (f *fn) prepareRootLoopRange(r *wasm.Reader) {
	if len(f.ctrl) != 1 || f.ctrl[0].kind != cfFunc || f.inlineDepth != 0 || f.localBase != 0 || f.rootAddressLoopScanned {
		return
	}
	f.rootAddressLoopScanned = true
	if !f.opt(optMemoryMinimumBounds) || f.nLocals <= f.nParams || f.nLocals > 64 || f.moduleEH || len(f.customInstructions) != 0 {
		return
	}
	memory, ok := f.memoryType(0)
	if !ok || memory.Limits.Addr64 || memory.Limits.Min == 0 {
		return
	}
	index := int(f.traceFuncIdx) - f.importedFunctionCount()
	if index < 0 || index >= len(f.m.Code) {
		return
	}
	body := f.m.Code[index].BodyBytes
	if len(body) > 16<<10 || f.wasmPC < f.tracePCBase {
		return
	}
	start := int(f.wasmPC - f.tracePCBase)
	begin := r.Offset()
	if start < 0 || begin < start || begin > len(body) {
		return
	}
	var valid uint64
	for i := f.nParams; i < f.nLocals; i++ {
		if f.localType[i] == mtI32 {
			valid |= uint64(1) << i
		}
	}
	prefix := wasm.ReaderFrom(body)
	var imm wasm.InstructionImmediate
	for prefix.Offset() < start {
		op, err := prefix.Byte()
		if err != nil || f.classifier.ClassifyInto(&prefix, op, &imm) != nil {
			return
		}
		if op == 0x21 || op == 0x22 {
			if int(imm.Index) >= f.nLocals {
				return
			}
			valid &^= uint64(1) << imm.Index
		}
	}
	if prefix.Offset() != start {
		return
	}
	zero := valid
	scan := *r
	depth := 0
	for scan.HasNext() {
		op, err := scan.Byte()
		if err != nil || f.classifier.ClassifyInto(&scan, op, &imm) != nil {
			return
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f:
			depth++
		case 0x0b:
			if depth == 0 {
				loopBody := body[begin:scan.Offset()]
				f.rootLoopRangeLocal, f.rootLoopRangeMaximum = f.proveRootLoopRange(loopBody, zero)
				if f.rootLoopRangeLocal != 0 {
					f.stats.peep("root-loop-address-range")
				}
				return
			}
			depth--
		}
	}
	return
}
