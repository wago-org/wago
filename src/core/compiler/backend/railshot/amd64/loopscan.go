//go:build amd64

package amd64

import (
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// memAccessSize returns the byte width a plain scalar memory instruction
// accesses. The bounds diagnostics use it to classify loop accesses.
func memAccessSize(op byte) int {
	switch op {
	case 0x2c, 0x2d, 0x30, 0x31, 0x3a, 0x3c:
		return 1
	case 0x2e, 0x2f, 0x32, 0x33, 0x3b, 0x3d:
		return 2
	case 0x28, 0x2a, 0x34, 0x35, 0x36, 0x38, 0x3e:
		return 4
	case 0x29, 0x2b, 0x37, 0x39:
		return 8
	}
	return 0
}

// walkLoopBody consumes validated instructions until the matching loop end and
// always restores r. The shared Wasm classifier is the only immediate decoder:
// try_table catch vectors, br_table labels, SIMD/atomic forms, and memory64
// offsets therefore cannot desynchronize this scan. No partial findings escape
// on failure.
func walkLoopBodyWithClassifier(r *wasm.Reader, m *wasm.Module, classifier wasm.ModuleInstructionClassifier, visit func(op byte, imm wasm.InstructionImmediate)) bool {
	start := r.Offset()
	defer func() { _ = r.JumpTo(start) }()
	depth := 0
	var imm wasm.InstructionImmediate
	for {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if m != nil {
			err = classifier.ClassifyInto(r, op, &imm)
		} else {
			err = wasm.ClassifyInstructionImmediateInto(r, op, &imm)
		}
		if err != nil {
			return false
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f: // block, loop, if, try_table
			depth++
		case 0x0b: // end
			if depth == 0 {
				return true
			}
			depth--
		}
		visit(op, imm)
	}
}

// scanLoopBody records locals assigned anywhere in one loop and whether the
// loop grows memory. valid is false on any classifier failure; callers then
// discard all findings and conservatively clear loop-sensitive facts.
func scanLoopBody(r *wasm.Reader, m *wasm.Module) (setLocals []uint16, hasGrow, valid bool) {
	return scanLoopBodyWithClassifier(r, m, wasm.NewModuleInstructionClassifier(m, true), nil)
}

func scanLoopBodyWithClassifier(r *wasm.Reader, m *wasm.Module, classifier wasm.ModuleInstructionClassifier, dst []uint16) (setLocals []uint16, hasGrow, valid bool) {
	setLocals = dst[:0]
	if setLocals == nil {
		setLocals = []uint16{}
	}
	indexesValid := true
	valid = walkLoopBodyWithClassifier(r, m, classifier, func(_ byte, imm wasm.InstructionImmediate) {
		switch imm.Kind {
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			if imm.Index > uint32(^uint16(0)) {
				indexesValid = false
				return
			}
			setLocals = append(setLocals, uint16(imm.Index))
		case wasm.InstrMemoryGrow:
			hasGrow = true
		}
	})
	if !valid || !indexesValid {
		return nil, false, false
	}
	slices.Sort(setLocals)
	setLocals = slices.Compact(setLocals)
	return setLocals, hasGrow, true
}
