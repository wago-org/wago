//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func tailLoopInlineCandidate(m *wasm.Module, h []funcHints, index int, policy CodegenPolicy) bool {
	ft, ok := m.LocalFuncType(index)
	if !ok || policy.CompactNative || !policy.EnabledOption(optTailLoopInline) || !policy.EnabledOption(optTailSelfLoop) || len(ft.Results) > 1 || len(m.Code[index].BodyBytes) > 128 || h[index].localCount > 5 || h[index].globalCount != 0 || h[index].flags.has(hintTouchesMemory|hintUsesBulkMem|hintHasLoop|hintModuleEH) {
		return false
	}
	proof := fn{m: m, ft: ft, traceFuncIdx: uint32(index + m.ImportedFuncCount()), policy: policy, classifier: wasm.NewModuleInstructionClassifier(m, true)}
	return proof.onlySelfTailTransfers(m.Code[index].BodyBytes) || tailPairInlineCandidate(m, h, index, policy)
}

func (f *fn) zeroTailInlineLocals(t *inlineTarget, base int) {
	for i := t.paramCount(); i < int(t.localTypeEnd-t.typeStart); i++ {
		if t.tailZeroMask()&(1<<i) == 0 {
			f.stats.peep("tail-inline-zero-elide")
			continue
		}
		f.pushValue(storage{kind: stConst, typ: f.localType[base+i], cval: 0})
		f.setLocal(nil, base+i, false)
	}
}

func (f *fn) sourceLabelFrame(label uint32) int {
	frame := len(f.ctrl) - 1 - int(label)
	if f.inlineTailSourceFrame != 0 && frame == f.inlineTailSourceFrame {
		return f.inlineRetFrame
	}
	if f.inlineTailLoopFrame != 0 && frame == f.inlineTailLoopFrame-1 {
		return frame - 1
	}
	return frame
}

func (f *fn) tailLoopEntryTestOffset() (int, bool) {
	if f.inlineTailLoopFrame != 0 {
		entry := f.ctrl[f.inlineTailLoopFrame-1].controlSite
		return entry, entry >= 0
	}
	return f.selfTailEntry, f.localTailLeaf && f.nLocals == f.nParams
}

func (f *fn) branchToInlineTailLoop(index int, lastAssignment int) {
	frame := &f.ctrl[index]
	f.convergeFrameBranchState(frame)
	depth := f.depth()
	f.flush()
	f.moveBranchValues(frame, depth, frame.branchArity())
	f.restoreBranchHandlers(index)
	if frame.controlSite == -2 && f.inlineTailConstants != nil && f.inlineTailConstants.entry != nil {
		entry := f.inlineTailConstants.entry
		entry.sites = append(entry.sites, f.a.JmpPlaceholder())
		return
	}
	if !f.threadSelfTailEntryTestAt(frame.controlSite, lastAssignment) {
		f.branchJump(frame)
	}
}
