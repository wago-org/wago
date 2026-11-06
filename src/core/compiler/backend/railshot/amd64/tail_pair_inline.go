//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A pair has one direct outgoing target per member, and those targets close
// exactly this cycle. Equal signatures and no declared locals let both bodies
// share one private parameter region. Other graphs keep ordinary tail calls.
func tailPairInlineCandidate(m *wasm.Module, h []funcHints, index int, policy CodegenPolicy) bool {
	imported := m.ImportedFuncCount()
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	member := func(i int) (*wasm.CompType, bool) {
		ft, ok := m.LocalFuncType(i)
		if !ok || ft == nil || i < 0 || i >= len(h) || len(m.Code[i].BodyBytes) > 64 || int(h[i].localCount) != len(ft.Params) || len(ft.Params) > 5 || len(ft.Results) > 1 || h[i].globalCount != 0 || !h[i].flags.has(hintHasTailCall) || h[i].flags.has(hintTouchesMemory|hintUsesBulkMem|hintHasLoop|hintModuleEH) {
			return nil, false
		}
		proof := fn{m: m, ft: ft, traceFuncIdx: uint32(i + imported), policy: policy, classifier: classifier}
		return ft, proof.onlyLocalTailTransfers(m.Code[i].BodyBytes)
	}
	first, ok := member(index)
	if !ok {
		return false
	}
	target, count, ok := soleDirectTailTarget(m.Code[index].BodyBytes, classifier)
	if !ok || count > 2 || target == uint32(index+imported) || target == ^uint32(0) || int(target) < imported {
		return false
	}
	other := int(target) - imported
	if other < 0 || other >= len(m.Code) {
		return false
	}
	second, ok := member(other)
	if !ok || len(first.Params) != len(second.Params) || len(first.Results) != len(second.Results) {
		return false
	}
	for i := range first.Params {
		if !wasm.EqualValType(first.Params[i], second.Params[i]) {
			return false
		}
	}
	for i := range first.Results {
		if !wasm.EqualValType(first.Results[i], second.Results[i]) {
			return false
		}
	}
	back, count, ok := soleDirectTailTarget(m.Code[other].BodyBytes, classifier)
	return ok && count <= 2 && back == uint32(index+imported)
}

func soleDirectTailTarget(body []byte, classifier wasm.ModuleInstructionClassifier) (uint32, int, bool) {
	r := wasm.ReaderFrom(body)
	target, count := uint32(0), 0
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return 0, 0, false
		}
		if op == 0x13 || op == 0x15 {
			return 0, 0, false
		}
		if op == 0x12 {
			next, err := r.U32()
			if err != nil || count != 0 && next != target {
				return 0, 0, false
			}
			target = next
			count++
		} else if classifier.SkipImmediate(&r, op) != nil {
			return 0, 0, false
		}
	}
	return target, count, count > 0
}

func (f *fn) configureInlineTailCycle(t *inlineTarget) {
	f.inlineTailRoot = uint32(t.globalIdx)
	f.inlineTailPartner = 0
	f.inlineTailCopyBytes = uint16(len(t.body))
	target, count, ok := soleDirectTailTarget(t.body, f.classifier)
	if !ok || target == f.inlineTailRoot {
		return
	}
	partner := f.inlineTargets.target(int(target))
	if partner == nil || !partner.tailLoop() {
		return
	}
	f.inlineTailPartner = target + 1
	// Reserve mandatory partner bodies as well as root bytes in the entry-copy
	// budget, even if constant folding later removes an arm.
	f.inlineTailCopyBytes = uint16(len(t.body) + count*len(partner.body))
}

func (f *fn) bindInlineTailTransfer(target *inlineTarget) (*inlineTailConstants, int) {
	parent := f.inlineTailConstants
	var constants *inlineTailConstants
	if parent != nil && (uint32(target.globalIdx) != f.inlineTailRoot || f.inlineTailExpandDepth < inlineTailExpansionDepth && f.inlineTailExpandBytes >= f.inlineTailCopyBytes) {
		constants = f.captureInlineTailConstants(target, f.localBase)
	}
	f.inlineTailConstants = nil
	lastAssignment := -1
	for i := target.paramCount() - 1; i >= 0; i-- {
		root := f.s.back()
		if root != nil && root.isValue() && root.st.kind == stLocalReg && root.st.index() == f.localBase+i {
			f.erase(root)
			continue
		}
		if i == 0 {
			lastAssignment = f.a.Len()
		}
		f.setLocal(nil, f.localBase+i, false)
	}
	f.zeroTailInlineLocals(target, f.localBase)
	f.inlineTailConstants = parent
	return constants, lastAssignment
}

// The complete range since the final argument assignment must still be the
// single ADD/SUB writing the tested register. Any emitted instruction, trap,
// frame movement, width mismatch or other destination retains TEST.
func (f *fn) reuseInlineTailArgumentFlags(reg Reg, wide bool) bool {
	start := f.inlineTailArgumentStart
	if f.inlineTailLoopFrame == 0 || !f.opt(optBranchFold) || start < 0 || start >= f.a.Len() || !selfTailAssignmentSetsZero(f.a.B[start:], reg, wide) {
		return false
	}
	f.stats.peep("inline-tail-argument-flags")
	return true
}
