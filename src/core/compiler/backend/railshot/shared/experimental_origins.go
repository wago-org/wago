package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const ExperimentMaxFunctionBytes = 4096

// experimentOrigins preserves precise memory-trap locations in repeated bodies
// and scalar fallbacks. Synthetic control instructions use the original header.
// The bounded instruction boundary tape prevents matches inside immediates.
func experimentOrigins(m *wasm.Module, body, out []byte, p *ReplicationPlan, vector bool) []uint32 {
	origins := make([]uint32, len(out))
	for i := range origins {
		origins[i] = uint32(p.Start)
	}
	for i := 0; i < p.Start; i++ {
		origins[i] = uint32(i)
	}
	suffix := len(out) - (len(body) - p.End)
	for i := suffix; i < len(out); i++ {
		origins[i] = uint32(p.End + i - suffix)
	}
	var boundary [ExperimentMaxFunctionBytes + ExperimentMaxFunctionGrowth + 1]bool
	r := wasm.ReaderFrom(out)
	classify := wasm.NewModuleInstructionClassifier(m, true)
	loadPC, storePC := uint32(p.Start), uint32(p.Start)
	for _, x := range p.Operations[:p.OperationN] {
		if x.Opcode == 0x28 || x.Opcode == 0x2a {
			loadPC = uint32(p.BodyStart) + uint32(x.Start)
		}
		if x.Opcode == 0x36 || x.Opcode == 0x38 {
			storePC = uint32(p.BodyStart) + uint32(x.Start)
		}
	}
	for r.BytesLeft() > 0 {
		at := r.Offset()
		boundary[at] = true
		op, err := r.Byte()
		if err != nil {
			break
		}
		var imm wasm.InstructionImmediate
		if classify.ClassifyInto(&r, op, &imm) != nil {
			break
		}
		if vector && at >= p.Start && at < suffix && op == 0xfd && (imm.Subopcode == 0 || imm.Subopcode == 11) {
			pc := loadPC
			if imm.Subopcode == 11 {
				pc = storePC
			}
			for i := at; i < r.Offset(); i++ {
				origins[i] = pc
			}
		}
	}
	boundary[len(out)] = true
	pattern := body[p.BodyStart:p.BodyEnd]
	if len(pattern) != 0 {
		for at := p.Start; at+len(pattern) <= suffix; at++ {
			if !boundary[at] || !boundary[at+len(pattern)] || !bytes.Equal(out[at:at+len(pattern)], pattern) {
				continue
			}
			for i := range pattern {
				origins[at+i] = uint32(p.BodyStart + i)
			}
			at += len(pattern) - 1
		}
	}
	return origins
}
