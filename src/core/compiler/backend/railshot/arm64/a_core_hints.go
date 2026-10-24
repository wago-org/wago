//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func loopWeight(depth int) int64 {
	if depth > maxLoopWeightDepth {
		depth = maxLoopWeightDepth
	}
	w := int64(1)
	for i := 0; i < depth; i++ {
		w *= loopWeightFactor
	}
	return w
}

func (f funcHintFlags) has(flag funcHintFlags) bool { return f&flag != 0 }

func (f *funcHintFlags) set(flag funcHintFlags) { *f |= flag }

func (f *funcHintFlags) assign(flag funcHintFlags, value bool) {
	if value {
		*f |= flag
	} else {
		*f &^= flag
	}
}

func (h funcHints) immediateFreeOpCount() uint16 {
	return h.immediateFreeOps
}

func (h funcHints) callRelocSiteCount() uint16 { return h.callRelocSites & callRelocSiteCountMask }

func (h funcHints) hasUnsupportedDynamicCall() bool {
	return h.callRelocSites&callRelocUnsupportedDynamicMask != 0
}

func (h funcHints) hasLoopIntConsts() bool {
	return h.callRelocSites&callRelocLoopIntConstMask != 0
}

func (h *funcHints) markLoopIntConsts() {
	h.callRelocSites |= callRelocLoopIntConstMask
}

func retainedLocalScoreCount(h funcHints) int {
	n := int(h.localCount)
	if n > 64 && !h.flags.has(hintIntervalRegionStorage) {
		return 64
	}
	return n
}

func (s funcHintSidecar) viewAt(h funcHints, function int) funcHintView {
	nLocals := int(h.localCount)
	localStart := int(h.localStart)
	localEnd := localStart + retainedLocalScoreCount(h)
	var localLastGet []uint32
	if s.localLastGetRangeCount == 0 && len(s.localLastGet) == len(s.localScore) {
		localLastGet = s.localLastGet[localStart:localEnd]
	} else if s.localLastGetRangeCount != 0 {
		// Sparse ranges are ordered by localStart because module hints are
		// appended in function order. Each pair at the front of localLastGet
		// names the dense score offset and its compact last-get offset. Keeping
		// ranges and values in one backing avoids another slice allocation.
		// Binary search remains independent of parallel worker scheduling.
		key := uint32(h.localStart)
		lo, hi := 0, int(s.localLastGetRangeCount)
		for lo < hi {
			mid := int(uint(lo+hi) >> 1)
			if s.localLastGet[mid*2] < key {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo < int(s.localLastGetRangeCount) && s.localLastGet[lo*2] == key {
			start := int(s.localLastGet[lo*2+1])
			localLastGet = s.localLastGet[start : start+nLocals]
		}
	}
	globalStart := int(h.globalStart)
	globalEnd := globalStart + int(h.globalCount)
	eventMeta := shared.FindLocalEventMeta(s.localEventMeta, h.localStart)
	shadow := shared.FindResidencyShadow(s.residencyShadow, h.localStart)
	view := funcHintView{
		skipInlineScan:  function >= 0 && function/64 < len(s.inlineScanUnneeded) && s.inlineScanUnneeded[function/64]&(uint64(1)<<uint(function%64)) != 0,
		funcHints:       h,
		nLocals:         nLocals,
		localScore:      s.localScore[localStart:localEnd],
		localLastGet:    localLastGet,
		sparseGlobals:   s.sparseGlobals[globalStart:globalEnd],
		localEventMeta:  eventMeta,
		residencyShadow: shadow,
	}
	// Loop constants are sparse. Most functions in application modules have no
	// loop at all, so avoid a binary search for entries they cannot own.
	if function < 0 || !h.hasLoopIntConsts() || len(s.loopIntConsts) == 0 {
		return view
	}
	lo, hi := 0, len(s.loopIntConsts)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if int(s.loopIntConsts[mid].function) < function {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(s.loopIntConsts) && int(s.loopIntConsts[lo].function) == function {
		entry := s.loopIntConsts[lo]
		view.loopIntConst = entry.bits
		view.loopIntConstTypes = entry.types
		view.loopIntConstCount = entry.count
	}
	return view
}

func (h *funcHints) noteControlDepth(depth int) {
	const depthMask = uint8(0x7f)
	flags := h.maxControlDepth &^ depthMask
	current := h.maxControlDepth & depthMask
	if depth >= int(depthMask) {
		current = depthMask
	} else if d := uint8(depth); d > current {
		current = d
	}
	h.maxControlDepth = flags | current
}

func (h funcHints) controlDepth() int {
	depth := h.maxControlDepth &^ hotScalarMergeBit
	if depth == 0x7f {
		return 255
	}
	return int(depth)
}

func (h funcHints) hasHotScalarMerge() bool { return h.maxControlDepth&hotScalarMergeBit != 0 }

func (h *funcHintView) addScalarMergeWeight(weight int64) {
	if weight <= 0 {
		return
	}
	if uint64(weight) >= uint64(^uint32(0)-h.scalarMergeWeight) {
		h.scalarMergeWeight = ^uint32(0)
	} else {
		h.scalarMergeWeight += uint32(weight)
	}
	if h.scalarMergeWeight >= hotScalarMergeThreshold {
		h.maxControlDepth |= hotScalarMergeBit
	}
}

func encodedScalarMergeBlockType(r wasm.Reader, m *wasm.Module) bool {
	x, err := r.S33()
	if err != nil {
		return false
	}
	switch x {
	case -1, -2, -3, -4: // i32, i64, f32, f64
		return true
	case -64: // empty block type
		return false
	}
	if x < 0 || m == nil {
		return false
	}
	ft, ok := m.TypeFunc(uint32(x))
	if !ok || len(ft.Results) != 1 {
		return false
	}
	typ := ft.Results[0]
	return typ == wasm.I32 || typ == wasm.I64 || typ == wasm.F32 || typ == wasm.F64
}

func (h *funcHintView) setLocalEventSummary(count int, overflow bool) {
	if count > int(localEventCountMask) {
		count = int(localEventCountMask)
	}
	h.localEventMeta = uint32(count)
	if overflow {
		h.localEventMeta |= localEventOverflow
	}
}

func (h funcHintView) localEventCount() int { return int(h.localEventMeta & localEventCountMask) }

func (h *funcHintView) noteLocalEvent(kind shared.LocalEventKind, local uint32, depth int) {
	if h.localEvents == nil && !h.countLocalEvents {
		return
	}
	if local > uint32(^uint16(0)-1) {
		if h.localEvents != nil {
			h.localEvents.Overflow = true
		} else {
			h.localEventFull = true
		}
		return
	}
	if h.localEvents != nil {
		h.localEvents.Append(kind, uint16(local), depth)
	} else if h.localEventCountAt < shared.LocalEventLimit {
		h.localEventCountAt++
	} else {
		h.localEventFull = true
	}
}

func (h *funcHintView) noteBoundaryEvent(kind shared.LocalEventKind, depth int) {
	if h.localEvents != nil {
		h.localEvents.Append(kind, shared.NoLocal, depth)
	} else if h.countLocalEvents && h.localEventCountAt < shared.LocalEventLimit {
		h.localEventCountAt++
	} else if h.countLocalEvents {
		h.localEventFull = true
	}
}

func (h *funcHintView) collectingLocalEvents() bool {
	return h.localEvents != nil || h.countLocalEvents
}

func funcHintsWithStorage(localScore []uint32) funcHintView {
	return funcHintView{nLocals: len(localScore), localScore: localScore}
}

func localHotness(score uint32) uint32 { return score & localScoreHotnessMask }

func loadDefinedLocalMask(scores []uint32) uint64 {
	var mask uint64
	for i, score := range scores[:min(len(scores), 64)] {
		if score&localScoreLoadDefined != 0 {
			mask |= uint64(1) << uint(i)
		}
	}
	return mask
}

func loadDefinedLocalMaskForHints(h *funcHintView) uint64 {
	// Load-defined provenance only affects address formation. Functions that do
	// not touch linear memory cannot consume it, so skip walking their local
	// scores during compilation.
	if !h.flags.has(hintTouchesMemory) {
		return 0
	}
	return loadDefinedLocalMask(h.localScore)
}

func (h *funcHintView) markEntryInitialized(idx uint32) {
	if idx >= 64 || int(idx) >= len(h.localScore) {
		return
	}
	h.entryInitialized |= uint64(1) << idx
	h.localScore[idx] |= localScoreEntryInitialized
}

func (h *funcHintView) markLoadDefined(idx uint32) {
	if int(idx) < len(h.localScore) {
		h.localScore[idx] |= localScoreLoadDefined
	}
}

func (h *funcHintView) noteParamAddress(idx uint32) {
	if idx >= 64 || int(idx) >= len(h.localScore) {
		return
	}
	bit := uint64(1) << idx
	if h.paramAddressSeen&bit != 0 {
		h.localScore[idx] |= localScoreParamAddressReuse
	} else {
		h.paramAddressSeen |= bit
	}
}

func addHotness(scores []uint32, idx uint32, delta int64) {
	if int(idx) >= len(scores) || delta <= 0 {
		return
	}
	flags, score := scores[idx]&^localScoreHotnessMask, localHotness(scores[idx])
	if uint64(score)+uint64(delta) >= uint64(localScoreHotnessMask) {
		scores[idx] = flags | localScoreHotnessMask
	} else {
		scores[idx] = flags | (score + uint32(delta))
	}
}

func addGlobalHotness(accum *shared.GlobalHintAccumulator, idx uint32, delta int64) {
	accum.Add(idx, delta)
}

func markGlobalEligible(accum *shared.GlobalHintAccumulator, idx uint32) {
	accum.MarkEligible(idx)
}

func newGlobalEligibilityTracker(nGlobals int) globalEligibilityTracker {
	return globalEligibilityTracker{marks: make([]uint32, nGlobals)}
}

func (t *globalEligibilityTracker) reset() {
	t.globals = t.globals[:0]
	t.frames = t.frames[:0]
}

func (t *globalEligibilityTracker) push() int {
	t.epoch++
	if t.epoch == 0 {
		for i := range t.marks {
			t.marks[i] = 0
		}
		t.epoch = 1
	}
	t.frames = append(t.frames, globalEligibilityFrame{start: len(t.globals), epoch: t.epoch})
	return len(t.frames) - 1
}

func (t *globalEligibilityTracker) add(frame int, global uint32) {
	if frame < 0 || frame >= len(t.frames) || int(global) >= len(t.marks) {
		return
	}
	epoch := t.frames[frame].epoch
	if t.marks[global] == epoch {
		return
	}
	t.marks[global] = epoch
	t.globals = append(t.globals, global)
}

func (t *globalEligibilityTracker) globalsIn(frame int) []uint32 {
	if frame < 0 || frame >= len(t.frames) {
		return nil
	}
	return t.globals[t.frames[frame].start:]
}

func (t *globalEligibilityTracker) pop(frame int) {
	if frame < 0 || frame != len(t.frames)-1 {
		return
	}
	start := t.frames[frame].start
	t.globals = t.globals[:start]
	t.frames = t.frames[:frame]
}

func scanFuncBodyIntoModule(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, branchHints []wasm.BranchHint, h funcHintView, elig *globalEligibilityTracker, m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, moduleHints []funcHints, importedFuncs int, globalHints *shared.GlobalHintAccumulator, collectLoopIntConsts bool) (funcHintView, error) {
	if len(fn.BodyBytes) != 0 {
		return scanBodyBytesIntoModule(fn.BodyBytes, fn.LocalDeclBytes, nLocals, nGlobals, selfIdx, branchHints, h, elig, m, classifier, moduleHints, nil, importedFuncs, globalHints, collectLoopIntConsts)
	}
	return scanBodyInto(fn.Body, nLocals, nGlobals, selfIdx, h, elig, m, globalHints), nil
}

func gcOrAtomicInstructionMayCall(kind wasm.InstrKind) bool {
	switch kind {
	// GC-reference table.set can enter the collector's write-barrier helper.
	// Conservatively include it even when table types/helper admission are absent.
	case wasm.InstrTableSet:
		return true
	case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructNewDesc, wasm.InstrStructNewDefaultDesc,
		wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructAtomicGet, wasm.InstrStructAtomicGetS, wasm.InstrStructAtomicGetU, wasm.InstrStructSet,
		wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData, wasm.InstrArrayNewElem,
		wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen,
		wasm.InstrArrayFill, wasm.InstrArrayCopy, wasm.InstrArrayInitData, wasm.InstrArrayInitElem,
		wasm.InstrRefGetDesc, wasm.InstrRefTest, wasm.InstrRefCast, wasm.InstrRefTestDesc, wasm.InstrRefCastDescEq, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail,
		wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny, wasm.InstrRefI31, wasm.InstrI31GetS, wasm.InstrI31GetU,
		wasm.InstrMemoryAtomicNotify, wasm.InstrMemoryAtomicWait32, wasm.InstrMemoryAtomicWait64:
		return true
	default:
		return false
	}
}

// usesBulkScratch describes ARM64 lowering, not just the Wasm memory proposal.
// These helpers write fixed registers in X9-X14 after flushing stack operands;
// that flush does not evict long-lived local/global pins. Segment drops matter
// even in modules with no memory or table, and table helpers share the same pool.
func usesBulkScratch(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrMemoryInit, wasm.InstrDataDrop, wasm.InstrMemoryCopy, wasm.InstrMemoryFill,
		wasm.InstrTableInit, wasm.InstrElemDrop, wasm.InstrTableCopy, wasm.InstrTableFill:
		return true
	default:
		return false
	}
}

func scanBodyBytesIntoModule(body []byte, localDeclBytes uint32, nLocals int, nGlobals int, selfIdx uint32, branchHints []wasm.BranchHint, h funcHintView, elig *globalEligibilityTracker, m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, moduleHints []funcHints, parallelCalls []parallelCalleeHints, importedFuncs int, globalHints *shared.GlobalHintAccumulator, collectLoopIntConsts bool) (funcHintView, error) {
	elig.reset()
	r := wasm.ReaderFrom(body)
	var cached wasm.ModuleInstructionClassifier
	if classifier != nil {
		cached = *classifier
	} else {
		cached = wasm.NewModuleInstructionClassifier(m, true)
	}
	s := byteBodyScanner{r: byteScanReader{Reader: r}, h: h, nLocals: nLocals, nGlobals: nGlobals, selfIdx: selfIdx, localDeclBytes: localDeclBytes, branchHints: branchHints, elig: elig, globalHints: globalHints, m: m, classifier: cached, moduleHints: moduleHints, parallelCalls: parallelCalls, importedFuncs: importedFuncs, entryPrefix: true, collectLoopIntConsts: collectLoopIntConsts}
	called, term, err := s.scanExpr(0, 0, -1, false, 1)
	if err != nil {
		return s.h, err
	}
	if called {
		s.h.flags.set(hintHasCall)
	}
	if term != 0x0b || s.r.has() {
		return s.h, s.r.err(wasm.ErrInvalidInstruction, s.r.off())
	}
	if s.loopIntConstN != 0 {
		s.finishLoopIntConsts()
	}
	return s.h, nil
}

func newLoopIntConstCandidate(bits int64, score uint64, typ uint8) loopIntConstCandidate {
	return loopIntConstCandidate{bits: bits, scoreType: min(score, loopIntConstScoreMask) | uint64(typ&3)<<62}
}

func (c loopIntConstCandidate) typ() uint8 { return uint8(c.scoreType >> 62) }

func (c loopIntConstCandidate) score() uint64 { return c.scoreType & loopIntConstScoreMask }

func (c *loopIntConstCandidate) addScore(score uint64) {
	current := c.score()
	if score > loopIntConstScoreMask-current {
		current = loopIntConstScoreMask
	} else {
		current += score
	}
	c.scoreType = current | uint64(c.typ())<<62
}

func intConstWideMoveCost(bits int64, typ uint8) int {
	words := 4
	v := uint64(bits)
	if typ == 1 {
		words, v = 2, uint64(uint32(bits))
	}
	zeros, ones := 0, 0
	for i := 0; i < words; i++ {
		h := uint16(v >> (16 * i))
		if h == 0 {
			zeros++
		} else if h == 0xffff {
			ones++
		}
	}
	cost := words - zeros
	if words-ones < cost {
		cost = words - ones
	}
	if cost == 0 {
		return 1
	}
	return cost
}

func loopIntConstNeedsRegister(next byte, bits int64, typ uint8) bool {
	switch next {
	case 0x6c, 0x7e: // i32/i64.mul have no immediate form.
		return true
	case 0x6a, 0x6b, 0x7c, 0x7d: // add/sub admit a signed +/- 12-bit magnitude.
		return bits < -0xfff || bits > 0xfff
	case 0x71, 0x72, 0x73: // i32 and/or/xor logical immediate.
		return typ == 1 && !a64.LogicalImmediate32(uint32(bits))
	case 0x83, 0x84, 0x85: // i64 and/or/xor logical immediate.
		return typ == 2 && !a64.LogicalImmediate64(uint64(bits))
	default:
		return false
	}
}

func (s *byteBodyScanner) noteLoopIntConst(bits int64, typ uint8, loopDepth int, pathWeight int64) {
	if loopDepth == 0 {
		return
	}
	cost := intConstWideMoveCost(bits, typ)
	score := uint64(pathWeight * loopWeight(loopDepth) * int64(cost))
	for i := 0; i < int(s.loopIntConstN); i++ {
		c := &s.loopIntConsts[i]
		if c.bits == bits && c.typ() == typ {
			c.addScore(score)
			return
		}
	}
	if int(s.loopIntConstN) == len(s.loopIntConsts) {
		return
	}
	s.loopIntConsts[s.loopIntConstN] = newLoopIntConstCandidate(bits, score, typ)
	s.loopIntConstN++
}

func (s *byteBodyScanner) finishLoopIntConsts() {
	for out := 0; out < len(s.h.loopIntConst); out++ {
		var best *loopIntConstCandidate
		for i := 0; i < int(s.loopIntConstN); i++ {
			c := &s.loopIntConsts[i]
			if c.typ() != 0 && (best == nil || c.score() > best.score()) {
				best = c
			}
		}
		if best == nil {
			break
		}
		s.h.loopIntConst[out] = best.bits
		s.h.loopIntConstTypes |= best.typ() << (2 * out)
		s.h.loopIntConstCount++
		best.scoreType = 0
	}
	if s.h.loopIntConstCount != 0 {
		s.h.markLoopIntConsts()
	}
}

func (s *byteBodyScanner) notePhysicalEvent(op byte, depth int) {
	var kind shared.LocalEventKind
	switch op {
	case 0x02, 0x1f:
		kind = shared.LocalEventBlock
	case 0x03:
		kind = shared.LocalEventLoop
	case 0x04:
		kind = shared.LocalEventIf
	case 0x05:
		kind = shared.LocalEventElse
	case 0x0b:
		kind = shared.LocalEventEnd
	case 0x08, 0x09, 0x0a, 0x0c, 0x0d, 0x0e, 0x0f:
		kind = shared.LocalEventBranch
	case 0x10, 0x11, 0x12, 0x13, 0x14, 0x15:
		kind = shared.LocalEventCall
	case 0x24, 0x26, 0x40:
		kind = shared.LocalEventInvalidate
	case 0xfd:
		kind = shared.LocalEventPressure
	default:
		return
	}
	s.h.noteBoundaryEvent(kind, depth)
}

func (s *byteBodyScanner) scanExpr(depth int, loopDepth int, curLoop int, stopAtElse bool, pathWeight int64) (bool, byte, error) {
	if depth > 20000 {
		return true, 0, s.r.err(wasm.ErrInstructionNestingLimitExceeded, s.r.off())
	}
	hotnessWeight := pathWeight * loopWeight(loopDepth)
	subHasCall := false
	var prevOp, prevPrevOp byte
	var prevIndex, prevPrevIndex uint32
	for {
		op, err := s.r.byte()
		if err != nil {
			return true, 0, err
		}

		curIndex := ^uint32(0)
		if s.h.collectingLocalEvents() {
			s.notePhysicalEvent(op, depth)
		}
		switch op {
		case 0x00: // unreachable
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
		case 0x0b: // end
			return subHasCall, op, nil
		case 0x05: // else
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
			if stopAtElse {
				return subHasCall, op, nil
			}
			return true, op, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
		case 0x02, 0x03, 0x04: // block, loop, if
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
			opOffset := s.localDeclBytes + uint32(s.r.off()-1)
			s.h.noteControlDepth(depth + 1)
			if loopDepth != 0 && op != 0x03 && encodedScalarMergeBlockType(s.r.Reader, s.m) {
				s.h.addScalarMergeWeight(pathWeight * loopWeight(loopDepth))
			}
			if err := wasm.SkipInstructionImmediate(&s.r.Reader, op); err != nil {
				return true, 0, err
			}
			switch op {
			case 0x02: // block
				calls, term, err := s.scanExpr(depth+1, loopDepth, curLoop, false, pathWeight)
				if err != nil {
					return true, 0, err
				}
				if term != 0x0b {
					return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
				subHasCall = subHasCall || calls
			case 0x03: // loop
				s.h.flags.set(hintHasLoop)
				loop := s.elig.push()
				calls, term, err := s.scanExpr(depth+1, loopDepth+1, loop, false, pathWeight)
				if err != nil {
					return true, 0, err
				}
				if term != 0x0b {
					return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
				if calls {
					subHasCall = true
				} else {
					for _, g := range s.elig.globalsIn(loop) {
						markGlobalEligible(s.globalHints, g)
					}
				}
				s.elig.pop(loop)
			case 0x04: // if
				thenWeight, elseWeight := pathWeight, pathWeight
				if likely, ok := s.branchHintAt(opOffset); ok {
					if likely {
						thenWeight = weightedBranchPath(thenWeight)
					} else {
						elseWeight = weightedBranchPath(elseWeight)
					}
				}
				callsThen, term, err := s.scanExpr(depth+1, loopDepth, curLoop, true, thenWeight)
				if err != nil {
					return true, 0, err
				}
				callsElse := false
				if term == 0x05 {
					callsElse, term, err = s.scanExpr(depth+1, loopDepth, curLoop, false, elseWeight)
					if err != nil {
						return true, 0, err
					}
				}
				if term != 0x0b {
					return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
				subHasCall = subHasCall || callsThen || callsElse
			}
		case 0x10, 0x12: // call, return_call
			idx, err := s.r.U32()
			if err != nil {
				return true, 0, err
			}
			s.h.flags.set(hintHasCall)
			if int(idx) < s.importedFuncs {
				s.h.flags.set(hintCallsImport)
			}
			if loopDepth != 0 {
				s.h.flags.set(hintHasLoopCall)
			}
			subHasCall = true
			if op == 0x10 && idx == s.selfIdx {
				s.h.flags.set(hintCallsSelf)
			}
			s.noteDirectCallRef(idx, op == 0x10, loopDepth != 0)
		case 0x11, 0x13: // indirect calls
			if err := s.scanIndirectCall(op, loopDepth); err != nil {
				return true, 0, err
			}
			subHasCall = true
		case 0x14, 0x15: // call_ref, return_call_ref
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
			s.h.flags.set(hintHasCall)
			s.h.flags.set(hintHasNonDirectCall)
			if op == 0x14 || op == 0x15 {
				s.h.markUnsupportedDynamicCall()
			}
			if loopDepth != 0 {
				s.h.flags.set(hintHasLoopCall)
			}
			subHasCall = true
		case 0x20, 0x21, 0x22: // local.get/set/tee
			idx, err := s.r.U32()
			if err != nil {
				return true, 0, err
			}
			curIndex = idx
			if int(idx) < s.nLocals {
				// Recognize a true dependent pointer recurrence:
				// local.get x; scalar load; local.set/tee x. Broader
				// "assigned by any load" marking retains dependency-breaking
				// renames in unrelated codec temporaries and loses their gain.
				if prevOp == 0x28 && op != 0x20 &&
					prevPrevOp == 0x20 && prevPrevIndex == idx {
					s.h.markLoadDefined(idx)
				}
				kind := shared.LocalEventDefine
				if op == 0x20 {
					kind = shared.LocalEventRead
				}
				s.h.noteLocalEvent(kind, idx, depth)
				if s.entryPrefix && idx < 64 {
					bit := uint64(1) << idx
					if s.entrySeen&bit == 0 {
						s.entrySeen |= bit
						if op != 0x20 {
							s.h.markEntryInitialized(idx)
						}
					}
				}
				if op == 0x20 {
					addHotness(s.h.localScore, idx, hotnessWeight)
					if int(idx) < len(s.h.localLastGet) {
						s.h.localLastGet[idx] = uint32(s.r.off())
					}
				} else {
					addHotness(s.h.localScore, idx, 2*hotnessWeight)
				}
			}
		case 0x23, 0x24: // global.get/set
			idx, err := s.r.U32()
			if err != nil {
				return true, 0, err
			}
			if int(idx) < s.nGlobals {
				if op == 0x24 {
					addGlobalHotness(s.globalHints, idx, 2*hotnessWeight)
				} else {
					addGlobalHotness(s.globalHints, idx, hotnessWeight)
				}
				s.elig.add(curLoop, idx)
			}
		case 0x41, 0x42: // i32.const, i64.const
			var bits int64
			var typ uint8
			if op == 0x41 {
				v, err := s.r.I32()
				if err != nil {
					return true, 0, err
				}
				bits, typ = int64(v), 1
			} else {
				v, err := s.r.I64()
				if err != nil {
					return true, 0, err
				}
				bits, typ = v, 2
			}
			if s.collectLoopIntConsts && loopDepth != 0 {
				if next, ok := s.r.Reader.Peek(); ok && loopIntConstNeedsRegister(next, bits, typ) {
					s.noteLoopIntConst(bits, typ, loopDepth, pathWeight)
				}
			}
		case 0x43: // f32.const
			s.h.flags.set(hintHasFloatConst)
			if _, err := s.r.Bytes(4); err != nil {
				return true, 0, err
			}
		case 0x44: // f64.const
			s.h.flags.set(hintHasFloatConst)
			if _, err := s.r.Bytes(8); err != nil {
				return true, 0, err
			}
		case 0x0c, 0x0d: // br, br_if
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
		case 0x0f: // return
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
		case 0x25, 0x26: // table.get/set
			index, err := s.r.U32()
			if err != nil {
				return true, 0, err
			}
			if op == 0x26 {
				s.h.flags.set(hintMutatesTable)
				if tableSetMayCall(s.m, index) {
					s.h.flags.set(hintHasCall | hintHasNonDirectCall)
					s.h.markUnsupportedDynamicCall()
					if loopDepth != 0 {
						s.h.flags.set(hintHasLoopCall)
					}
					subHasCall = true
				}
			}
		case 0xd2, 0xd5, 0xd6: // ref.func, br_on_null, br_on_non_null
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
			if op != 0xd2 {
				s.h.flags.set(hintHasControlFlow)
				s.entryPrefix = false
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0xfc, 0xfd, 0xfe, 0xfb:
			calls, err := s.scanMemoryInstruction(op, depth, loopDepth, prevOp, prevIndex)
			if err != nil {
				return true, 0, err
			}
			subHasCall = subHasCall || calls
		case 0x1f: // try_table: blocktype, catch vector, body
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
			s.h.flags.set(hintModuleEH)
			s.h.noteControlDepth(depth + 1)
			if err := wasm.SkipInstructionImmediate(&s.r.Reader, op); err != nil {
				return true, 0, err
			}
			calls, term, err := s.scanExpr(depth+1, loopDepth, curLoop, false, pathWeight)
			if err != nil {
				return true, 0, err
			}
			if term != 0x0b {
				return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
			}
			subHasCall = subHasCall || calls
		case 0x08: // throw
			s.h.flags.set(hintModuleEH)
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
		case 0x0a: // throw_ref
			s.h.flags.set(hintModuleEH)
		default:
			if _, ok := wasm.ImmediateFreeInstructionKind(op); ok {
				if s.h.immediateFreeOpCount() < defaultStackArenaCap {
					s.h.immediateFreeOps++
				}
				break
			}
			if err := s.scanOtherInstruction(op); err != nil {
				return true, 0, err
			}
		}
		prevPrevOp, prevPrevIndex = prevOp, prevIndex
		prevOp, prevIndex = op, curIndex
	}
}

// Keep immediate records and uncommon proposal handling off the recursive
// control scanner's stack. These helpers preserve its exact classification
// and event rules while ordinary scalar/control walking uses a smaller frame.
func (s *byteBodyScanner) scanMemoryInstruction(op byte, depth, loopDepth int, prevOp byte, prevIndex uint32) (hasCall bool, err error) {
	if op == 0xfd {
		s.h.flags.set(hintModuleSIMD)
	}
	var imm wasm.InstructionImmediate
	err = s.classifyInstructionInto(op, &imm)
	if err != nil {
		return true, err
	}
	return s.noteMemoryInstruction(op, depth, loopDepth, prevOp, prevIndex, &imm), nil
}

func (s *byteBodyScanner) noteMemoryInstruction(op byte, depth, loopDepth int, prevOp byte, prevIndex uint32, imm *wasm.InstructionImmediate) (hasCall bool) {
	if isTableMutation(imm.Kind) {
		s.h.flags.set(hintMutatesTable)
	}
	if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
		s.h.flags.set(hintHasControlFlow)
	}
	if shared.InstructionNeedsEHFrame(op, imm.Kind) {
		s.h.flags.set(hintModuleEH)
	}
	if op == 0xfb {
		s.h.noteBoundaryEvent(shared.LocalEventCollection, depth)
		// Collector-backed GC instructions may enter the synchronous Go
		// helper bridge. Preserve LR and use call-safe local state for the
		// whole family; direct-only subopcodes pay only the frame-record cost.
		s.h.flags.set(hintHasCall)
		s.h.flags.set(hintHasNonDirectCall)
		s.h.markUnsupportedDynamicCall()
		if loopDepth != 0 {
			s.h.flags.set(hintHasLoopCall)
		}
		hasCall = true
	}
	switch imm.Kind {
	case wasm.InstrMemoryGrow:
		if !shared.SharedMemoryGrowMayCall(s.m, imm.Kind, imm.Index) {
			break
		}
		fallthrough
	case wasm.InstrMemoryAtomicNotify, wasm.InstrMemoryAtomicWait32, wasm.InstrMemoryAtomicWait64:
		s.h.noteBoundaryEvent(shared.LocalEventCollection, depth)
		s.h.flags.set(hintHasCall)
		s.h.flags.set(hintHasNonDirectCall)
		s.h.markUnsupportedDynamicCall()
		if loopDepth != 0 {
			s.h.flags.set(hintHasLoopCall)
		}
		hasCall = true
	}
	if imm.TouchesMemory {
		s.h.flags.set(hintTouchesMemory)
		s.h.addMemOp()

		if isExactBounds4Opcode(op) && imm.MemOffset == 0 && (!imm.HasMemIndex || imm.MemIndex == 0) {
			s.h.addBounds4Op()
		}
		if op >= 0x28 && op <= 0x35 && prevOp == 0x20 {
			s.h.noteParamAddress(prevIndex)
		}
	}
	if usesBulkScratch(imm.Kind) {
		s.h.noteBoundaryEvent(shared.LocalEventInvalidate, depth)
		s.h.flags.set(hintUsesBulkMem)
	}
	return hasCall
}

// The immediate record must stay in this helper's frame rather than being
// replicated at every recursive control depth.
//
//go:noinline
func (s *byteBodyScanner) scanIndirectCall(op byte, loopDepth int) error {
	var imm wasm.InstructionImmediate
	if err := s.classifyInstructionInto(op, &imm); err != nil {
		return err
	}
	s.h.flags.set(hintHasCall)
	s.h.flags.set(hintHasNonDirectCall)
	if loopDepth != 0 {
		s.h.flags.set(hintHasLoopCall)
	}
	return nil
}

func (s *byteBodyScanner) scanOtherInstruction(op byte) error {
	var imm wasm.InstructionImmediate
	err := s.classifyInstructionInto(op, &imm)
	if err != nil {
		return err
	}
	if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
		s.h.flags.set(hintHasControlFlow)
		if op == 0x0e { // br_table is the only unprefixed boundary in this path.
			s.entryPrefix = false
		}
	}
	if shared.InstructionNeedsEHFrame(op, imm.Kind) {
		s.h.flags.set(hintModuleEH)
	}
	if imm.TouchesMemory {
		s.h.flags.set(hintTouchesMemory)
		s.h.addMemOp()
	}
	if usesBulkScratch(imm.Kind) {
		s.h.flags.set(hintUsesBulkMem)
	}
	return nil
}

func (s *byteBodyScanner) noteDirectCallRef(globalIdx uint32, inline, inLoop bool) {
	local := int(globalIdx) - s.importedFuncs
	if local < 0 || local >= len(s.moduleHints) && local >= len(s.parallelCalls) {
		return
	}
	if count := s.h.callRelocSiteCount(); count != callRelocSiteCountMask {
		s.h.callRelocSites = s.h.callRelocSites&(callRelocLoopIntConstMask|callRelocUnsupportedDynamicMask) | count + 1
	}
	if len(s.parallelCalls) != 0 {
		target := &s.parallelCalls[local]
		target.direct.Add(1)
		if inline {
			target.inline.Add(1)
			if inLoop {
				target.loop.Store(true)
			}
		}
		return
	}
	target := &s.moduleHints[local]
	if target.directCallRefs != ^uint8(0) {
		target.directCallRefs++
	}
	if inline && target.inlineCallSites != ^uint16(0) {
		target.inlineCallSites++
	}
	if inline && inLoop {
		target.flags.set(hintHasInlineLoopCall)
	}
}

func (s *byteBodyScanner) branchHintAt(offset uint32) (bool, bool) {
	for i := range s.branchHints {
		if s.branchHints[i].Offset == offset {
			return s.branchHints[i].Likely, true
		}
		if s.branchHints[i].Offset > offset {
			break
		}
	}
	return false, false
}

func (s *byteBodyScanner) classifyInstructionInto(op byte, imm *wasm.InstructionImmediate) error {
	var err error
	if s.m != nil {
		err = s.classifier.ClassifyInto(&s.r.Reader, op, imm)
	} else {
		start := s.r.Offset()
		err = wasm.ClassifyInstructionImmediateInto(&s.r.Reader, op, imm)
		if err != nil {
			// Legacy unit scanners may not carry a module. Retain their validated
			// single-memory memory64 retry without weakening module-aware walks.
			s.r.JumpTo(start)
			err = wasm.ClassifyInstructionImmediateIntoWithMemarg64(&s.r.Reader, op, imm, true)
		}
	}
	if err == nil && isTableMutation(imm.Kind) {
		s.h.flags.set(hintMutatesTable)
	}
	return err
}

func isTableMutation(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrTableSet, wasm.InstrTableInit, wasm.InstrTableCopy,
		wasm.InstrTableGrow, wasm.InstrTableFill:
		return true
	default:
		return false
	}
}

func (r *byteScanReader) has() bool { return r.HasNext() }

func (r *byteScanReader) off() int { return r.Offset() }

func (r *byteScanReader) byte() (byte, error) { return r.Byte() }

func shouldSkipStackFence(hasCall bool, nLocalSlots int, bodyBytesLen int) bool {
	return !hasCall && frameHdrBytes+8*nLocalSlots+8*bodyBytesLen <= 4096
}

func (h funcHints) memOpCount() uint32 { return h.memOps & memOpCountMask }

func (h funcHints) bounds4OpCount() uint32 { return h.memOps >> memOpCountBits & bounds4OpMask }

func (h *funcHintView) addMemOp() {
	if h.memOpCount() != memOpCountMask {
		h.memOps++
	}
}

func (h *funcHintView) addBounds4Op() {
	if h.bounds4OpCount() != bounds4OpMask {
		h.memOps += 1 << memOpCountBits
	}
}

func isExactBounds4Opcode(op byte) bool {
	switch op {
	case 0x28, 0x2a, 0x34, 0x35, 0x36, 0x38, 0x3e:
		return true
	default:
		return false
	}
}
