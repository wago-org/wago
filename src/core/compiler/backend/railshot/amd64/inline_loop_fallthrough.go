//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Internal loops may trap or never return. Every reachable function exit must
// be its final end, however: no return or branch may address the synthetic
// function label. Only private integer state is touched by this bounded class.
func tupleLoopHasOnlyInternalBranches(t *inlineTarget, classifier wasm.ModuleInstructionClassifier) bool {
	if t == nil || !t.tupleLoop() || len(t.body) == 0 || len(t.body) > 64 {
		return false
	}
	r := wasm.NewReader(t.body)
	depth, sawLoop := 0, false
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x0b {
			if depth == 0 {
				return sawLoop && !r.HasNext()
			}
			depth--
			continue
		}
		integer := op == 0x41 || op == 0x42 || op >= 0x45 && op <= 0x5a || op >= 0x67 && op <= 0x8a ||
			op == 0xa7 || op == 0xac || op == 0xad || op >= 0xc0 && op <= 0xc4
		if !(op >= 0x01 && op <= 0x05 || op == 0x0c || op == 0x0d ||
			op >= 0x1a && op <= 0x1c || op >= 0x20 && op <= 0x22 || integer) ||
			classifier.ClassifyInto(r, op, &imm) != nil {
			return false
		}
		if (op == 0x0c || op == 0x0d) && imm.Index >= uint32(depth) {
			return false
		}
		if op == 0x05 && depth == 0 {
			return false
		}
		if op == 0x02 || op == 0x03 || op == 0x04 {
			if depth == 8 {
				return false
			}
			depth++
			sawLoop = sawLoop || op == 0x03
		}
	}
	return false
}

func (f *fn) inlineTupleLoopFallthrough(t *inlineTarget) bool {
	return f.inlineDepth == 1 && f.inlineTupleLoopFallthroughState(t)
}

func (f *fn) inlineTupleLoopFallthroughState(t *inlineTarget) bool {
	return f.opt(optStraightLineBlock) && !profileEnabled && !f.interruptible &&
		!f.makesCalls && !f.usesCalls && !f.moduleEH && f.gcFrameRoots == nil && len(f.customInstructions) == 0 &&
		len(f.intervalLast) == 0 && !f.intervalControl && !f.pinRelinquished && !f.policy.CompactNative &&
		(f.stats == nil || !f.stats.RecordSources) && tupleLoopHasOnlyInternalBranches(t, f.classifier)
}
