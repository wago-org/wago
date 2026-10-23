//go:build arm64

package arm64

import (
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func inlineOK(f inlineFacts, policy CodegenPolicy) bool {
	switch {
	case f.moduleEH:
		return false
	case policy.CompactNative:
		return compactInlineOK(f, policy)
	case f.hasControlCall, f.calleeCount > 0, !f.regABIIntOnly:
		return false
	case f.hasLoop:
		return false
	case f.bodyBytes > inlineMaxBytes:
		return false
	default:
		return true
	}
}

// envDefaultOn parses a default-on (opt-out) boolean knob: empty/unset means
// enabled; 0/false/off/no disables it.
func envDefaultOn(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func (t *inlineTarget) recursive() bool { return t.params&inlineRecursiveFlag != 0 }

func (t *inlineTarget) paramCount() int { return int(t.params &^ inlineRecursiveFlag) }

func (ts inlineTargetTable) target(globalIdx int) *inlineTarget {
	localIdx := globalIdx - ts.first
	if ts.data == nil || localIdx < 0 || localIdx >= len(ts.data.slots) {
		return nil
	}
	slot := ts.data.slots[localIdx]
	if slot == 0 || slot&inlineTargetInvalid != 0 {
		return nil
	}
	return &ts.data.targets[slot-1]
}

func (ts inlineTargetTable) empty() bool { return ts.data == nil || len(ts.data.targets) == 0 }

func (ts inlineTargetTable) localTypes(t *inlineTarget) []machineType {
	return ts.data.types[t.typeStart:t.localTypeEnd:t.localTypeEnd]
}

func (ts inlineTargetTable) omitStandaloneBody(localIdx int, hostAdapter bool) bool {
	if !inlineDeadBodyEnabled || hostAdapter {
		return false
	}
	t := ts.target(ts.first + localIdx)
	return t != nil && t.omitStandalone
}

// buildInlineTargets returns the straight-line leaf inline candidates keyed by
// GLOBAL function index, or an empty table when inlining is disabled. Candidacy
// here is a property of the callee alone (the call-site count only mattered for the report),
// so a call to one of these can be spliced wherever it appears.
func buildInlineTargets(m *wasm.Module, allHints []funcHints, policy CodegenPolicy) inlineTargetTable {
	if !policy.EnabledOption(optInline) {
		return inlineTargetTable{}
	}
	hasCall := false
	for i := range allHints {
		if allHints[i].flags.has(hintHasCall) {
			hasCall = true
			break
		}
	}
	if !hasCall {
		return inlineTargetTable{}
	}
	importedFuncs := m.ImportedFuncCount()
	if uint64(len(m.Code)) > uint64(^uint32(0)) {
		return inlineTargetTable{}
	}
	candidateCount, typeCount := 0, 0
	for i := range m.Code {
		ft, _, ok := inlineTargetFacts(m, allHints, i, policy)
		if !ok {
			continue
		}
		candidateCount++
		add := int(allHints[i].localCount) + len(ft.Results)
		if add < 0 || typeCount > int(^uint32(0))-add {
			// Inlining is optional. Fall back to direct calls instead of retaining a
			// sidecar whose compact 32-bit ranges cannot represent this module.
			return inlineTargetTable{}
		}
		typeCount += add
	}
	if candidateCount == 0 || candidateCount >= int(inlineTargetInvalid) {
		return inlineTargetTable{}
	}
	data := &inlineTargetData{
		slots:   make([]uint32, len(m.Code)),
		targets: make([]inlineTarget, 0, candidateCount),
		types:   make([]machineType, 0, typeCount),
	}
	targets := inlineTargetTable{first: importedFuncs, data: data, classifier: wasm.NewModuleInstructionClassifier(m, true)}
	for i := range m.Code {
		ft, facts, ok := inlineTargetFacts(m, allHints, i, policy)
		if !ok {
			continue
		}
		h := allHints[i]
		localStart := len(data.types)
		for _, p := range ft.Params {
			data.types = append(data.types, mtOf(p))
		}
		for _, run := range m.Code[i].Locals.Runs {
			for k := 0; k < int(run.Count); k++ {
				data.types = append(data.types, mtOf(run.Type))
			}
		}
		localEnd := len(data.types)
		for _, result := range ft.Results {
			data.types = append(data.types, mtOf(result))
		}
		resultEnd := len(data.types)
		res0 := mtNone
		if len(ft.Results) > 0 {
			res0 = data.types[localEnd]
		}
		params := uint32(facts.params)
		if recursiveInlineCandidate(m, allHints, i, facts, policy) {
			params |= inlineRecursiveFlag
		}
		data.targets = append(data.targets, inlineTarget{
			body:           m.Code[i].BodyBytes,
			globalIdx:      importedFuncs + i,
			localDeclBytes: m.Code[i].LocalDeclBytes,
			typeStart:      uint32(localStart),
			localTypeEnd:   uint32(localEnd),
			resultTypeEnd:  uint32(resultEnd),
			params:         params,
			res0:           res0,
			touchesMem:     facts.touchesMem,
			touchesGlob:    facts.touchesGlobal,
			hasCtrl:        facts.hasControlFlow,
			omitStandalone: policy.CompactNative &&
				h.inlineCallSites == 1 && h.directCallRefs == 1 && !h.flags.has(hintHasInlineLoopCall),
		})
		data.slots[i] = uint32(len(data.targets))
	}
	if policy.CompactNative {
		pruneNestedSizeInlineTargets(m, &targets)
	}
	return targets
}

func inlineTargetFacts(m *wasm.Module, allHints []funcHints, i int, policy CodegenPolicy) (*wasm.CompType, inlineFacts, bool) {
	body := m.Code[i].BodyBytes
	if len(body) == 0 {
		return nil, inlineFacts{}, false
	}
	ft, ok := m.LocalFuncType(i)
	if !ok || ft == nil {
		return nil, inlineFacts{}, false
	}
	h := allHints[i]
	if h.flags.has(hintModuleEH) {
		return nil, inlineFacts{}, false
	}
	facts := inlineFacts{
		bodyBytes:      len(body),
		hasLoop:        h.flags.has(hintHasLoop),
		hasControlFlow: h.flags.has(hintHasControlFlow),
		touchesGlobal:  h.globalCount != 0,
		touchesMem:     h.flags.has(hintTouchesMemory | hintUsesBulkMem),
		params:         len(ft.Params),
		results:        len(ft.Results),
		declaredLocals: int(h.localCount) - len(ft.Params),
		callSites:      int(h.inlineCallSites),
		regABIIntOnly:  sigFitsRegABI(ft) && sigIsIntOnly(ft),
	}
	if h.flags.has(hintHasCall) {
		facts.calleeCount = 1
	}
	return ft, facts, inlineOK(facts, policy) || recursiveInlineCandidate(m, allHints, i, facts, policy)
}

func recursiveInlineCandidate(m *wasm.Module, allHints []funcHints, i int, facts inlineFacts, policy CodegenPolicy) bool {
	if !recursiveInlineEnabled || policy.CompactNative || len(m.Code) != 1 || m.ImportedFuncCount() != 0 || i != 0 {
		return false
	}
	h := allHints[i]
	return h.flags.has(hintCallsSelf) && !h.flags.has(hintHasLoop|hintTouchesMemory|hintUsesBulkMem|hintHasNonDirectCall|hintCallsImport|hintModuleEH) &&
		facts.regABIIntOnly && facts.declaredLocals == 0 && facts.bodyBytes <= 64 && h.callRelocSiteCount() <= 2
}

// inlineInLoopIsRegressive identifies a tiny stateless leaf whose native
// register-ABI call is cheaper than repeatedly materializing the inliner's
// synthetic local area in a caller loop. Keep the direct call in that case; it
// still benefits from the call-preserving leaf ABI in call.go.
func (t *inlineTarget) inlineInLoopIsRegressive() bool {
	return int(t.localTypeEnd-t.typeStart) == t.paramCount() && !t.touchesMem && !t.touchesGlob && !t.hasCtrl
}

// reserveInlineLocals scans the caller body for calls to inline targets and, for
// each distinct spliced callee, reserves its params+locals as fresh frame locals
// PAST f.nLocals (so the prologue's zeroDeclaredLocals — bounded by f.nLocals —
// never touches them; each splice binds/zeroes them itself). Records the base for
// callOp. Must run after assignPinnedLocals (which sizes f.locals): the reserved
// locals are appended unpinned. All splice sites of the same callee share one
// region — inlined bodies never overlap (a straight-line leaf fully completes
// before the next splice), so the region is safely reused.
func (f *fn) reserveInlineLocals(callees []*inlineTarget, targets inlineTargetTable) {
	if len(callees) == 0 {
		return
	}
	f.inlineTargets = targets
	if len(callees) <= maxRetainedInlineBases {
		clear(f.inlineBasePool)
		if f.inlineBasePool == nil {
			f.inlineBasePool = make(map[int]int, len(callees))
		}
		f.inlineBase = f.inlineBasePool
	} else {
		f.inlineBase = make(map[int]int, len(callees))
	}
	for _, t := range callees {
		base := len(f.localType)
		for _, lt := range targets.localTypes(t) {
			f.localType = append(f.localType, lt)
			// compileFuncAttempt rejects the completed frame before any of these
			// homes are consumed. Accepted native frames are far below uint32
			// slots, so this representation is exact on every successful path.
			f.localSlot = append(f.localSlot, uint32(f.nLocalSlots))
			f.nLocalSlots += lt.stackSlots()
			f.locals = append(f.locals, localDef{reg: regNone, state: lsMem})
		}
		f.inlineBase[t.globalIdx] = base
	}
}

// collectInlinedCallees scans the caller body once and returns the distinct
// inline targets it calls, in first-call order. Computed before frame setup so
// the caller's guard-page pin exclusion can be re-derived from the callees
// (whether any touches memory), and reused by reserveInlineLocals.
func collectInlinedCallees(caller *wasm.Func, targets inlineTargetTable) []*inlineTarget {
	if targets.empty() || len(caller.BodyBytes) == 0 {
		return nil
	}
	var out []*inlineTarget
	var smallSeen [inlineLinearSeenTargets]int
	seenN := 0
	var largeSeen map[int]struct{}
	r := wasm.NewReader(caller.BodyBytes)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return out
		}
		if _, ok := wasm.ImmediateFreeInstructionKind(op); ok {
			continue
		}
		var t *inlineTarget
		if op == 0x10 { // call
			idx, err := r.U32()
			if err != nil {
				return out
			}
			t = targets.target(int(idx))
		} else {
			switch op {
			case 0x05, 0x0b: // else, end
				continue
			case 0x08, 0x0c, 0x0d, 0x12, 0x14, 0x15,
				0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0xd2, 0xd5, 0xd6:
				if _, err := r.U32(); err != nil {
					return out
				}
				continue
			case 0x41:
				if _, err := r.I32(); err != nil {
					return out
				}
				continue
			case 0x42:
				if _, err := r.I64(); err != nil {
					return out
				}
				continue
			case 0x43:
				if _, err := r.Bytes(4); err != nil {
					return out
				}
				continue
			case 0x44:
				if _, err := r.Bytes(8); err != nil {
					return out
				}
				continue
			}
			if err := targets.classifier.ClassifyInto(r, op, &imm); err != nil {
				return out
			}
			if imm.Kind != wasm.InstrCall {
				continue
			}
			t = targets.target(int(imm.Index))
		}
		if t == nil {
			continue
		}
		duplicate := false
		if largeSeen != nil {
			_, duplicate = largeSeen[t.globalIdx]
		} else {
			for _, globalIdx := range smallSeen[:seenN] {
				if globalIdx == t.globalIdx {
					duplicate = true
					break
				}
			}
		}
		if duplicate {
			continue
		}
		if largeSeen != nil {
			largeSeen[t.globalIdx] = struct{}{}
		} else if seenN < len(smallSeen) {
			smallSeen[seenN] = t.globalIdx
			seenN++
		} else {
			largeSeen = make(map[int]struct{}, 2*len(smallSeen))
			for _, globalIdx := range smallSeen {
				largeSeen[globalIdx] = struct{}{}
			}
			largeSeen[t.globalIdx] = struct{}{}
		}
		out = append(out, t)
	}
	return out
}

// allCallsWillInline reports whether every call in caller is a direct call that
// the current inline plan will splice. This lets frame/register planning treat
// such a caller as truly call-free: no BL remains to clobber LR or local pins.
// Calls in loops honor the same regression guard as callOp.
func allCallsWillInline(caller *wasm.Func, targets inlineTargetTable, policy CodegenPolicy) bool {
	if targets.empty() || len(caller.BodyBytes) == 0 {
		return false
	}
	r := wasm.NewReader(caller.BodyBytes)
	var imm wasm.InstructionImmediate
	var controlLoops uint64 // one bit per ordinary control frame
	controlDepth := 0
	var deepControls []bool // pathological depth beyond the fixed 64-bit stack
	loopDepth := 0
	sawCall := false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if err := targets.classifier.ClassifyInto(r, op, &imm); err != nil {
			return false
		}
		switch op {
		case 0x02, 0x04: // block, if
			if controlDepth < 64 {
				controlLoops &^= uint64(1) << controlDepth
			} else {
				deepControls = append(deepControls, false)
			}
			controlDepth++
		case 0x03: // loop
			if controlDepth < 64 {
				controlLoops |= uint64(1) << controlDepth
			} else {
				deepControls = append(deepControls, true)
			}
			controlDepth++
			loopDepth++
		case 0x0b: // end (the final function end has no matching entry here)
			if controlDepth != 0 {
				controlDepth--
				wasLoop := false
				if controlDepth < 64 {
					bit := uint64(1) << controlDepth
					wasLoop = controlLoops&bit != 0
					controlLoops &^= bit
				} else {
					i := controlDepth - 64
					wasLoop = deepControls[i]
					deepControls = deepControls[:i]
				}
				if wasLoop {
					loopDepth--
				}
			}
		}
		switch imm.Kind {
		case wasm.InstrCall:
			sawCall = true
			t := targets.target(int(imm.Index))
			if t == nil || t.recursive() || (loopDepth != 0 && t.inlineInLoopIsRegressive()) {
				return false
			}
		case wasm.InstrReturnCall, wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef:
			return false
		}
	}
	return sawCall
}

// inlinePlanTouchesMemory reports whether any spliced callee touches linear
// memory — used to extend the caller's touchesMemory for the guard-page pin
// exclusion, since the spliced memory ops run in the caller's frame.
func inlinePlanTouchesMemory(callees []*inlineTarget) bool {
	for _, t := range callees {
		if t.touchesMem {
			return true
		}
	}
	return false
}

// inlineCall splices target t's body at the current call site: it binds the p
// argument operands into the callee's param locals, zeroes the callee's declared
// locals, runs the body with localBase set (remapping the callee's locals onto
// the reserved region), then decouples the results from the reserved slots so a
// later splice of the same callee cannot alias them. A straight-line callee runs
// on the cheap frameless path; a control-flow callee runs under a synthetic
// boundary frame (inlineBodyCtrl).
func (f *fn) inlineCall(t *inlineTarget) error {
	f.stats.call(callKindInline)
	start := f.a.Len()
	base := f.inlineBase[t.globalIdx]
	f.bindInlineParams(t, base)

	old := f.localBase
	oldTraceFunc, oldTraceBase, oldPC := f.traceFuncIdx, f.tracePCBase, f.wasmPC
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		previous := f.enterProfileInline()
		defer f.leaveProfileInline(previous)
	}
	f.localBase = base
	f.traceFuncIdx, f.tracePCBase = uint32(t.globalIdx), t.localDeclBytes
	oldInlineDepth := f.inlineDepth
	f.inlineDepth++
	var err error
	if t.hasCtrl {
		err = f.inlineBodyCtrl(t)
	} else {
		err = f.inlineBody(t.body)
	}
	f.localBase = old
	f.inlineDepth = oldInlineDepth
	f.traceFuncIdx, f.tracePCBase, f.wasmPC = oldTraceFunc, oldTraceBase, oldPC
	if err != nil {
		return err
	}

	// The callee's results sit on the operand stack; a bare `local.get` result is a
	// lazy stLocalRef into a reserved slot, and a deferred result may read one. A
	// later splice of the same callee rebinds those slots, so realize any operand
	// still referencing the reserved region into a register/value now. (The control-
	// flow path already merged results into canonical slots, so this is a no-op there.)
	nLocals := int(t.localTypeEnd - t.typeStart)
	f.realizeInlineRange(base, base+nLocals)
	f.stats.addInlineSiteBytes(f.a.Len() - start)
	return nil
}

// bindInlineParams binds the p argument operands into the callee's param locals
// and zero-initializes its declared locals (wasm zero-init, re-done each splice
// so a call in a loop or a second site always starts from zero). Each declared
// local is cleared across its full slot width (a v128 local clears both halves).
func (f *fn) bindInlineParams(t *inlineTarget, base int) {
	nLocals := int(t.localTypeEnd - t.typeStart)
	params := t.paramCount()
	// The p args are the top operands (deepest = param 0). Pop each into its param
	// local. setLocal takes the absolute index (localBase is still 0 here).
	for i := params - 1; i >= 0; i-- {
		f.setLocal(nil, base+i, false)
	}
	if nLocals > params {
		z := f.allocReg(0)
		// arm64: zero a register via MOVZ #0 (no flag side-effect, unlike x86's
		// xor-self). f.st64 hides the scaled-offset encodability fallback for the
		// frame store (§6.1: never call the raw Store64, which returns ok bool).
		f.a.MovImm64(z, 0)
		localTypes := f.inlineTargets.localTypes(t)
		for i := params; i < nLocals; i++ {
			for s := 0; s < localTypes[i].stackSlots(); s++ {
				f.st64(SP, f.localOff(base+i)+int32(8*s), z)
			}
			f.locals[base+i].state = lsMem
		}
		f.release(z)
	}
}

// inlineBody runs a straight-line callee body's opcodes on the current operand
// stack (with localBase already set), stopping at the terminating `end`. The
// callee is a leaf with no control flow, so every opcode is a plain (non-control)
// op that emitPlain lowers; results are left on the operand stack.
func (f *fn) inlineBody(body []byte) error {
	r := wasm.NewReader(body)
	for {
		f.wasmPC = f.tracePCBase + uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			return err
		}
		switch op {
		case 0x0b: // terminating end: results are on the stack
			return nil
		case 0x01: // nop — the driver's body() handles this outside emitPlain
			continue
		}
		var previous profileOrigin
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			previous = f.enterProfileInstruction()
		}
		err = f.emitPlain(r, op)
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			f.switchProfileOrigin(previous)
		}
		if err != nil {
			return err
		}
	}
}

// realizeInlineRange forces any operand-stack entry that references a reserved
// inline local in [lo, hi) into a register/value, so it no longer depends on that
// slot's contents (mirrors realizeLocalRefs, over a range).
func (f *fn) realizeInlineRange(lo, hi int) {
	inRange := func(idx int) bool { return idx >= lo && idx < hi }
	for e := f.s.next(f.s.head); e != f.s.head; {
		next := f.s.next(e)
		switch {
		case e.elemKind() == ekValue && (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && inRange(e.st.index()):
			f.materializeByType(e)
		case e.elemKind() == ekValue && e.st.kind == stMemRef && (inRange(e.st.memBorrow()) || inRange(e.st.memAliasLocal())):
			f.materializeByType(e)
		case e.elemKind() == ekDeferred && subtreeRefsLocalRange(f.s, e, lo, hi):
			f.condense(e, regNone)
		}
		e = next
	}
}

// subtreeRefsLocalRange reports whether the valent block rooted at e reads any
// local in [lo, hi).
func subtreeRefsLocalRange(s *stack, e *elem, lo, hi int) bool {
	if e == nil {
		return false
	}
	if e.elemKind() == ekValue {
		return (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && e.st.index() >= lo && e.st.index() < hi
	}
	if e.elemKind() == ekDeferred {
		return subtreeRefsLocalRange(s, s.arg0(e), lo, hi) || subtreeRefsLocalRange(s, s.arg1(e), lo, hi)
	}
	return false
}
