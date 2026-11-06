//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var tailSelfLoopEnabled = true

// This physical-leaf proof is deliberately limited to helper-free integer
// bodies. Source self calls remain logically unbounded even though no native
// call or frame growth remains. Unknown instructions keep ordinary tail entry.
func (f *fn) onlySelfTailTransfers(body []byte) bool {
	return f.integerTailTransfers(body, true)
}

func (f *fn) onlyLocalTailTransfers(body []byte) bool {
	return f.integerTailTransfers(body, false)
}

func (f *fn) integerTailTransfers(body []byte, selfOnly bool) bool {
	if !f.opt(optTailSelfLoop) || !f.opt(optRegABI) || profileEnabled || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || !sigFitsRegABI(f.ft) || !sigIsIntOnly(f.ft) || len(f.ft.Results) > 1 {
		return false
	}
	for _, run := range f.m.Code[int(f.traceFuncIdx)-f.importedFunctionCount()].Locals.Runs {
		if !wasm.EqualValType(run.Type, wasm.I32) && !wasm.EqualValType(run.Type, wasm.I64) {
			return false
		}
	}
	r := wasm.ReaderFrom(body)
	found := false
	previousConst, slot := false, uint64(0)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		constant := previousConst
		previousConst = false
		if op == 0x41 {
			v, err := r.I32()
			if err != nil {
				return false
			}
			previousConst, slot = true, uint64(uint32(v))
			continue
		}
		if op == 0x13 {
			typ, err := r.U32()
			if err != nil {
				return false
			}
			table, err := r.U32()
			if err != nil || !constant {
				return false
			}
			target, ok := f.constantPrivateTailTarget(typ, table, slot)
			if !ok || target != int(f.traceFuncIdx) {
				return false
			}
			found = true
			continue
		}
		if op == 0x12 {
			index, err := r.U32()
			if err != nil || selfOnly && index != f.traceFuncIdx || int(index) < f.importedFunctionCount() {
				return false
			}
			ft, ok := f.functionSignature(index)
			if !ok || !sigFitsRegABI(ft) || !sigIsIntOnly(ft) || !tailResultABICompatible(f.ft.Results, ft.Results) {
				return false
			}
			found = true
			continue
		}
		ordinary := op <= 0x05 || op >= 0x0b && op <= 0x0f ||
			op >= 0x1a && op <= 0x1c || op >= 0x20 && op <= 0x22 ||
			op == 0x41 || op == 0x42 || op >= 0x45 && op <= 0x5a ||
			op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad || op >= 0xc0 && op <= 0xc4
		if !ordinary || f.classifier.SkipImmediate(&r, op) != nil {
			return false
		}
	}
	return found
}

func (f *fn) emitSelfTailLoop() {
	// setLocal realizes every older read before overwriting a parameter. Reverse
	// assignment therefore preserves simultaneous arguments and register cycles.
	lastAssignment := -1
	for i := f.nParams - 1; i >= 0; i-- {
		root := f.s.back()
		if root != nil && root.isValue() && root.st.kind == stLocalReg && root.st.index() == i {
			f.erase(root)
			continue
		}
		if i == 0 {
			lastAssignment = f.a.Len()
		}
		f.setLocal(nil, i, false)
	}
	// Evaluating an earlier argument can relinquish a pin already assigned to
	// a later parameter. Both tail edges bypass parameter homing at entry.
	f.restoreCallFreePins()
	if !f.threadSelfTailEntryTest(lastAssignment) {
		f.a.PatchRel32(f.a.JmpPlaceholder(), f.selfTailEntry)
	}
	f.hasLoop = true
	if f.sc != nil {
		f.sc.directPreparedBounded = false
	}
	f.unreachable = true
	f.stats.peep("tail-self-loop")
}
