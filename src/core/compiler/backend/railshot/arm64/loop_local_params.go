//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var loopLocalParamsEnabled = true

// The proof excludes nested loops, so only one optimized loop can be active.
// Keep this cold data off the ordinary control-frame and merge arenas.
type loopLocalParamHomes struct {
	depth uint32 // control-frame index plus one
	local [4]uint16
}

func (f *fn) planLoopLocalParams(r *wasm.Reader, fr *ctrlFrame, signature wasm.Reader) (loopLocalParamHomes, int, bool) {
	var homes loopLocalParamHomes
	if !f.opt(optLoopLocalParams) || profileEnabled || f.interruptible || f.makesCalls || f.usesCalls ||
		f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 ||
		len(f.intervalLast) != 0 || f.loopLocalParams.depth != 0 ||
		f.policy.CompactNative || fr.kind != cfLoop || fr.paramN < 1 || fr.paramN > len(homes.local) || f.depth() < fr.paramN ||
		f.stats != nil && f.stats.RecordSources {
		return homes, 0, false
	}
	index, err := signature.I64()
	if err != nil || index < 0 || uint64(index) > uint64(^uint32(0)) {
		return homes, 0, false
	}
	ft, ok := f.m.TypeFunc(uint32(index))
	if !ok || !sigIsIntOnly(ft) {
		return homes, 0, false
	}
	peek := *r
	for i := 0; i < fr.paramN; i++ {
		op, err := peek.Byte()
		if err != nil || op != 0x21 {
			return homes, 0, false
		}
		x, err := peek.U32()
		absolute := uint64(x) + uint64(f.localBase)
		if err != nil || absolute > 65535 || absolute >= uint64(len(f.localType)) || f.localType[absolute] != mtOf(ft.Params[fr.paramN-1-i]) {
			return homes, 0, false
		}
		_, isFloat, pinned := f.pinReg(int(absolute))
		if !pinned || isFloat {
			return homes, 0, false
		}
		for _, previous := range homes.local[:i] {
			if previous == uint16(absolute) {
				return homes, 0, false
			}
		}
		homes.local[i] = uint16(absolute)
	}
	afterBindings := peek.Offset()
	depth := 0
	sawBackedge := false
	var imm wasm.InstructionImmediate
	for n := 0; n < 128 && peek.Offset()-afterBindings < 512; n++ {
		op, err := peek.Byte()
		if err != nil {
			return homes, 0, false
		}
		if op == 0x0b {
			if depth == 0 {
				return homes, afterBindings, sawBackedge
			}
			depth--
			continue
		}
		// Calls, nested loops, memory, globals, EH, references and tables keep
		// canonical transfers. Conditional backedges need a taken-edge plan.
		if !(op == 0x01 || op == 0x02 || op == 0x04 || op == 0x05 || op == 0x0c || op == 0x0d ||
			op >= 0x1a && op <= 0x1c || op >= 0x20 && op <= 0x22 || op >= 0x41 && op <= 0xc4) ||
			f.classifier.ClassifyInto(&peek, op, &imm) != nil {
			return homes, 0, false
		}
		if (op == 0x0c || op == 0x0d) && imm.Index == uint32(depth) {
			if op == 0x0d {
				return homes, 0, false
			}
			sawBackedge = true
		}
		if op == 0x02 || op == 0x04 {
			if depth == 8 {
				return homes, 0, false
			}
			depth++
		}
	}
	return homes, 0, false
}

// Consume parameters from top to bottom, as the leading local.set sequence
// would. setLocal preserves outstanding reads before each write, including
// overlapping deferred argument trees and rotations of destination locals.
func (f *fn) bindLoopLocalParams(homes loopLocalParamHomes, count int) {
	for _, local := range homes.local[:count] {
		if local < 64 {
			f.localWritten |= uint64(1) << local
		}
		f.setLocal(nil, int(local), false)
		f.setFactsForLocal(int(local), 0)
	}
}
