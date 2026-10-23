//go:build arm64

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"sync/atomic"
)

var validationHintStreamEnabled = true

type moduleValidationHints struct {
	module    *wasm.Module
	policy    CodegenPolicy
	hints     []funcHints
	sidecar   funcHintSidecar
	globals   []int64
	start     func(int) *validationHintStream
	end       func(int)
	finish    func() bool
	err       error
	completed bool
	functions int
	taken     uint32
}

func (v *moduleValidationHints) BeginFunction(i int) wasm.ValidationHintInstructionObserver {
	p := v.start(i)
	if p == nil {
		return nil
	}
	return p
}
func (v *moduleValidationHints) EndFunction(i int) { v.end(i) }
func (v *moduleValidationHints) Complete() bool    { v.completed = v.finish(); return v.completed }

// NewValidationHintObserver prepares the optional serial streaming consumer.
// Unavailable modes decline reuse and preserve the established scanner.
func NewValidationHintObserver(m *wasm.Module, opts CompileOptions) wasm.ValidationHintObserver {
	if m == nil || len(m.Code) == 0 || opts.Workers > 1 || diagnosticsEnabled && (opts.Stats != nil || explainEnabled) || len(opts.CustomInstructions) != 0 {
		return nil
	}
	for i := range m.Code {
		if len(m.Code[i].BodyBytes) == 0 {
			return nil
		}
	}
	selection, err := optimizationBindings.ResolveSnapshot(opts.Optimizations, opts.OptimizationSnapshot, opts.OptimizationDeltas)
	if err != nil {
		return nil
	}
	policy := shared.DefaultCodegenPolicy(selection)
	if !nativeCompactionDisabled && (opts.CompactNative || nativeCompactionEnabled) {
		policy = shared.CompactCodegenPolicy(selection)
	}
	if policy.FunctionAlignLog2 < 2 {
		policy.FunctionAlignLog2 = 2
	}
	if policy.InternalAlignLog2 < 2 {
		policy.InternalAlignLog2 = 2
	}
	if policy.LoopAlignLog2 < 2 {
		policy.LoopAlignLog2 = 2
	}
	if !policy.EnabledOption(optValidationHintStream) {
		return nil
	}
	collector := newModuleValidationHints(m, policy)
	if collector == nil {
		return nil
	}
	return collector
}

func takeValidationHints(m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis, policy CodegenPolicy, workers int, detailed bool) ([]funcHints, funcHintSidecar, []int64, bool) {
	if workers > 1 || detailed || !analysis.ValidFor(m) {
		return nil, funcHintSidecar{}, nil, false
	}
	collector, ok := analysis.HintObserver().(*moduleValidationHints)
	if !ok || collector.module != m || !collector.completed || collector.err != nil || collector.policy != policy || !atomic.CompareAndSwapUint32(&collector.taken, 0, 1) {
		return nil, funcHintSidecar{}, nil, false
	}
	return collector.hints, collector.sidecar, collector.globals, true
}

func newModuleValidationHints(m *wasm.Module, policy CodegenPolicy) *moduleValidationHints {
	nGlobals, importedFuncs := m.GlobalCount(), m.ImportedFuncCount()
	const detailedResidency = false
	out := &moduleValidationHints{module: m, policy: policy}
	n := len(m.Code)
	allHints := make([]funcHints, n)
	totalScores := 0
	intervalLocals := 0
	intervalFunctions := 0
	eventReserve := 0
	moduleEH := m.TagCount() != 0
	storageModuleEH := moduleEH
	for i := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok {
			return nil
		}
		count, err := countLocals(ft.Params, m.Code[i].Locals)
		if err != nil {
			return nil
		}
		intervalStorage := intervalRegionHintStorageEligible(policy.EnabledOption(optIntervalRegionPins), len(m.Code[i].BodyBytes), count, storageModuleEH)
		scoreCount := count
		if count > 64 && !intervalStorage {
			scoreCount = 64
		}
		if scoreCount > int(^uint(0)>>1)-totalScores {
			return nil
		}
		allHints[i].localCount = uint16(count)
		allHints[i].flags.assign(hintIntervalRegionStorage, intervalStorage)
		totalScores += scoreCount
		if intervalStorage {
			if count > int(^uint(0)>>1)-intervalLocals {
				return nil
			}
			intervalLocals += count
			intervalFunctions++
			eventReserve = max(eventReserve, min(shared.LocalEventInitialCapacity, len(m.Code[i].BodyBytes)/2))
		}
	}
	if uint64(totalScores) > uint64(^uint32(0)) || uint64(intervalLocals) > uint64(^uint32(0)) {
		return nil
	}
	localScores := make([]uint32, totalScores)
	// Dense storage needs no per-function index. Use sparse ranges only when
	// their index plus eligible-local payload is strictly smaller, so this
	// representation cannot increase retained last-get bytes on modules where
	// most functions qualify for interval regions.
	denseLastGets := uint64(totalScores) * 4
	sparseLastGets := uint64(intervalLocals)*4 + uint64(intervalFunctions)*8
	compactLastGets := sparseLastGets < denseLastGets
	lastGetCount := totalScores
	if compactLastGets {
		lastGetCount = intervalLocals + intervalFunctions*2
	}
	localLastGets := make([]uint32, lastGetCount)
	localEventMeta := make([]uint32, intervalFunctions*2)
	// Shadow summaries are telemetry only; ordinary codegen needs the event
	// counts above but should not reserve an array of zero diagnostic records.
	var residencyShadow []shared.ResidencyShadowEntry
	if detailedResidency {
		residencyShadow = make([]shared.ResidencyShadowEntry, intervalFunctions)
	}
	var sparseGlobals []shared.GlobalHint
	var loopIntConsts []loopIntConstHintEntry
	var agg []int64
	if nGlobals > 0 && n > 0 {
		agg = make([]int64, nGlobals)
	}
	intervalRangeAt := 0
	collectLoopIntConsts := policy.EnabledOption(optLoopIntConst)
	var sparseAccum shared.GlobalHintAccumulator
	eligibilityTracker := newGlobalEligibilityTracker(nGlobals)
	scoreAt := 0
	intervalAt := intervalFunctions * 2
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var localEvents shared.LocalEventTape
	if detailedResidency {
		localEvents.Events = make([]shared.LocalEvent, 0, eventReserve)
	}
	intervalEventAt := 0

	var h funcHintView
	var nLocals, scoreCount, currentIndex int
	var intervalStorage bool
	stream := &validationHintStream{}
	currentIndex = -1
	out.start = func(i int) *validationHintStream {
		if out.err != nil || i != out.functions {
			out.err = fmt.Errorf("out-of-order validation hints")
			return nil
		}
		currentIndex = i
		nLocals = int(allHints[i].localCount)
		intervalStorage = allHints[i].flags.has(hintIntervalRegionStorage)
		scoreCount = retainedLocalScoreCount(allHints[i])
		sparseAccum.Reset(nGlobals)
		h = funcHintsWithStorage(localScores[scoreAt : scoreAt+scoreCount])
		if compactLastGets {
			if intervalStorage {
				h.localLastGet = localLastGets[intervalAt : intervalAt+nLocals]
				localLastGets[intervalRangeAt] = uint32(scoreAt)
				localLastGets[intervalRangeAt+1] = uint32(intervalAt)
				intervalRangeAt += 2
				intervalAt += nLocals
			}
		} else {
			if intervalStorage {
				h.localLastGet = localLastGets[scoreAt : scoreAt+nLocals]
			}
		}
		h.nLocals = nLocals
		h.localCount = uint16(nLocals)
		h.localStart = uint32(scoreAt)
		if intervalStorage {
			if detailedResidency {
				localEvents.Reset(shared.LocalEventLimit)
				h.localEvents = &localEvents
			} else {
				h.countLocalEvents = true
			}
		}
		h.inlineCallSites = allHints[i].inlineCallSites
		h.directCallRefs = allHints[i].directCallRefs
		h.flags.assign(hintHasInlineLoopCall, allHints[i].flags.has(hintHasInlineLoopCall))

		eligibilityTracker.reset()
		stream.scanner = byteBodyScanner{r: byteScanReader{Reader: wasm.ReaderFrom(m.Code[i].BodyBytes)}, h: h, nLocals: nLocals, nGlobals: nGlobals, selfIdx: uint32(importedFuncs + i), localDeclBytes: m.Code[i].LocalDeclBytes, branchHints: m.BranchHintsForFunc(uint32(importedFuncs + i)), elig: &eligibilityTracker, globalHints: &sparseAccum, m: m, classifier: classifier, moduleHints: allHints, importedFuncs: importedFuncs, entryPrefix: true, collectLoopIntConsts: collectLoopIntConsts}
		stream.body = m.Code[i].BodyBytes
		stream.frames = append(stream.frames[:0], validationHintFrame{curLoop: -1, weight: 1, hotness: 1})
		stream.failed, stream.ended = false, false
		return stream
	}
	out.end = func(i int) {
		if out.err != nil {
			return
		}
		if currentIndex != i || stream.failed || !stream.ended || len(stream.frames) != 0 {
			out.err = fmt.Errorf("incomplete validation hints")
			return
		}
		if stream.scanner.loopIntConstN != 0 {
			stream.scanner.finishLoopIntConsts()
		}
		h = stream.scanner.h
		h.inlineCallSites = allHints[i].inlineCallSites
		h.directCallRefs = allHints[i].directCallRefs
		h.flags.assign(hintHasInlineLoopCall, allHints[i].flags.has(hintHasInlineLoopCall))
		if intervalStorage {
			count, overflow := h.localEventCountAt, h.localEventFull
			if detailedResidency {
				count, overflow = len(localEvents.Events), localEvents.Overflow
			}
			h.setLocalEventSummary(count, overflow)
			localEventMeta[intervalEventAt*2] = h.localStart
			localEventMeta[intervalEventAt*2+1] = h.localEventMeta
			if detailedResidency {
				residencyShadow[intervalEventAt] = shared.ResidencyShadowEntry{LocalStart: h.localStart, Summary: summarizeHintResidency(&localEvents, nLocals)}
			}
			intervalEventAt++
		}
		h.flags.assign(hintIntervalRegionStorage, intervalStorage)
		if h.loopIntConstCount != 0 {
			loopIntConsts = append(loopIntConsts, loopIntConstHintEntry{function: uint32(i), bits: h.loopIntConst, types: h.loopIntConstTypes, count: h.loopIntConstCount})
		}
		scoreAt += scoreCount
		moduleEH = moduleEH || h.flags.has(hintModuleEH)
		allHints[i] = h.funcHints
		start := len(sparseGlobals)
		sparseGlobals = sparseAccum.AppendTo(sparseGlobals)
		if uint64(len(sparseGlobals)) > uint64(^uint32(0)) {
			out.err = fmt.Errorf("function hint global sidecar exceeds 32-bit index capacity")
			return
		}
		allHints[i].globalStart = uint32(start)
		allHints[i].globalCount = uint32(len(sparseGlobals) - start)
		for _, gh := range sparseGlobals[start:] {
			agg[gh.Index] += int64(gh.Score)
		}
		out.functions++
	}
	out.finish = func() bool {
		if out.err != nil || out.functions != n {
			return false
		}
		moduleSIMD := false
		for i := range allHints {
			moduleSIMD = moduleSIMD || allHints[i].flags.has(hintModuleSIMD)
		}
		if moduleEH || moduleSIMD {
			for i := range allHints {
				allHints[i].flags.assign(hintModuleEH, moduleEH)
				allHints[i].flags.assign(hintModuleSIMD, moduleSIMD)
			}
		}
		if moduleEH && !storageModuleEH {
			localScores = compactEHLocalScores(allHints, localScores)
			localLastGets = nil
			localEventMeta = nil
			residencyShadow = nil
			intervalRangeAt = 0
		}
		out.hints = allHints
		out.sidecar = funcHintSidecar{
			localScore:             localScores,
			localLastGet:           localLastGets,
			localEventMeta:         localEventMeta,
			residencyShadow:        residencyShadow,
			loopIntConsts:          loopIntConsts,
			sparseGlobals:          sparseGlobals,
			localLastGetRangeCount: uint32(intervalRangeAt / 2),
		}
		out.globals = agg
		return true

	}
	return out
}

type validationHintFrame struct {
	loopDepth, curLoop       int
	weight, elseWeight       int64
	hotness                  int64
	prevIndex, prevPrevIndex uint32
	prevOp, prevPrevOp, kind byte
	hasCall, inElse          bool
}

type validationHintStream struct {
	scanner       byteBodyScanner
	body          []byte
	frames        []validationHintFrame
	failed, ended bool
}

func (v *validationHintStream) Observe(op byte, kind wasm.InstrKind, index, index2 uint32, bits uint64, meta uint8, start, end int) {
	if v.failed {
		return
	}
	if len(v.frames) == 0 || start < 0 || end <= start || end > len(v.body) {
		v.failed = true
		return
	}
	s := &v.scanner
	depth := len(v.frames) - 1
	f := &v.frames[depth]
	loopDepth := f.loopDepth
	weight := f.hotness

	currentIndex := ^uint32(0)
	if s.h.collectingLocalEvents() {
		s.notePhysicalEvent(op, depth)
	}
	switch op {
	case 0x00:
		s.h.flags.set(hintHasControlFlow)
		s.entryPrefix = false
	case 0x0b:
		if depth == 0 {
			if f.hasCall {
				s.h.flags.set(hintHasCall)
			}
			v.frames = v.frames[:0]
			v.ended = end == len(v.body)
			return
		}
		child := *f
		if child.kind == 0x03 {
			if !child.hasCall {
				for _, g := range s.elig.globalsIn(child.curLoop) {
					markGlobalEligible(s.globalHints, g)
				}
			}
			s.elig.pop(child.curLoop)
		}
		v.frames = v.frames[:depth]
		parent := &v.frames[depth-1]
		parent.hasCall = parent.hasCall || child.hasCall
		return
	case 0x05:
		s.h.flags.set(hintHasControlFlow)
		s.entryPrefix = false
		if f.kind != 0x04 || f.inElse {
			v.failed = true
			return
		}
		f.inElse = true
		f.weight = f.elseWeight
		f.hotness = f.weight * loopWeight(loopDepth)
		f.prevOp, f.prevPrevOp, f.prevIndex, f.prevPrevIndex = 0, 0, 0, 0
		return
	case 0x02, 0x03, 0x04, 0x1f:
		s.h.flags.set(hintHasControlFlow)
		s.entryPrefix = false
		s.h.noteControlDepth(depth + 1)
		if depth+1 > 20000 {
			v.failed = true
			return
		}
		if op == 0x1f {
			s.h.flags.set(hintModuleEH)
		}
		if loopDepth != 0 && op != 0x03 && op != 0x1f {
			if err := s.r.Reader.JumpTo(start + 1); err != nil {
				v.failed = true
				return
			}
			if encodedScalarMergeBlockType(s.r.Reader, s.m) {
				s.h.addScalarMergeWeight(f.hotness)
			}
		}
		child := validationHintFrame{kind: op, loopDepth: loopDepth, curLoop: f.curLoop, weight: f.weight, elseWeight: f.weight}
		if op == 0x03 {
			s.h.flags.set(hintHasLoop)
			child.loopDepth++
			child.curLoop = s.elig.push()
		}
		if op == 0x04 {
			if likely, ok := s.branchHintAt(s.localDeclBytes + uint32(start)); ok {
				if likely {
					child.weight = weightedBranchPath(child.weight)
				} else {
					child.elseWeight = weightedBranchPath(child.elseWeight)
				}
			}
		}
		f.prevPrevOp, f.prevPrevIndex = f.prevOp, f.prevIndex
		f.prevOp, f.prevIndex = op, currentIndex
		child.hotness = child.weight * loopWeight(child.loopDepth)
		v.frames = append(v.frames, child)
		return
	case 0x10, 0x12:
		s.h.flags.set(hintHasCall)
		if int(index) < s.importedFuncs {
			s.h.flags.set(hintCallsImport)
		}
		if loopDepth != 0 {
			s.h.flags.set(hintHasLoopCall)
		}
		f.hasCall = true
		if op == 0x10 && index == s.selfIdx {
			s.h.flags.set(hintCallsSelf)
		}
		s.noteDirectCallRef(index, op == 0x10, loopDepth != 0)
	case 0x11, 0x13:
		s.h.flags.set(hintHasCall | hintHasNonDirectCall)
		if loopDepth != 0 {
			s.h.flags.set(hintHasLoopCall)
		}
		f.hasCall = true
	case 0x14, 0x15:
		s.h.flags.set(hintHasCall | hintHasNonDirectCall)
		s.h.markUnsupportedDynamicCall()
		if loopDepth != 0 {
			s.h.flags.set(hintHasLoopCall)
		}
		f.hasCall = true
	case 0x20, 0x21, 0x22:
		currentIndex = index
		if int(index) < s.nLocals {
			if f.prevOp == 0x28 && op != 0x20 && f.prevPrevOp == 0x20 && f.prevPrevIndex == index {
				s.h.markLoadDefined(index)
			}
			eventKind := shared.LocalEventDefine
			if op == 0x20 {
				eventKind = shared.LocalEventRead
			}
			s.h.noteLocalEvent(eventKind, index, depth)
			if s.entryPrefix && index < 64 {
				bit := uint64(1) << index
				if s.entrySeen&bit == 0 {
					s.entrySeen |= bit
					if op != 0x20 {
						s.h.markEntryInitialized(index)
					}
				}
			}
			if op == 0x20 {
				addHotness(s.h.localScore, index, weight)
				if int(index) < len(s.h.localLastGet) {
					s.h.localLastGet[index] = uint32(end)
				}
			} else {
				addHotness(s.h.localScore, index, 2*weight)
			}
		}
	case 0x23, 0x24:
		if int(index) < s.nGlobals {
			if op == 0x24 {
				addGlobalHotness(s.globalHints, index, 2*weight)
			} else {
				addGlobalHotness(s.globalHints, index, weight)
			}
			s.elig.add(f.curLoop, index)
		}
	case 0x41, 0x42:
		typ := uint8(1)
		if op == 0x42 {
			typ = 2
		}
		if s.collectLoopIntConsts && loopDepth != 0 && end < len(v.body) && loopIntConstNeedsRegister(v.body[end], int64(bits), typ) {
			s.noteLoopIntConst(int64(bits), typ, loopDepth, f.weight)
		}
	case 0x43, 0x44:
		s.h.flags.set(hintHasFloatConst)
	case 0x0c, 0x0d, 0x0f:
		s.h.flags.set(hintHasControlFlow)
		s.entryPrefix = false
	case 0x25, 0x26:
		if op == 0x26 {
			s.h.flags.set(hintMutatesTable)
			if tableSetMayCall(s.m, index) {
				s.h.flags.set(hintHasCall | hintHasNonDirectCall)
				s.h.markUnsupportedDynamicCall()
				if loopDepth != 0 {
					s.h.flags.set(hintHasLoopCall)
				}
				f.hasCall = true
			}
		}
	case 0xd2, 0xd5, 0xd6:
		if op != 0xd2 {
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
		}
	case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0xfc, 0xfd:
		if op == 0xfd {
			s.h.flags.set(hintModuleSIMD)
		}
		imm := wasm.InstructionImmediate{Kind: kind, Index: index, Index2: index2, TouchesMemory: meta&wasm.ValidationHintTouchesMemory != 0, HasMemIndex: meta&wasm.ValidationHintHasMemoryIndex != 0, MemIndex: index, MemOffset: bits}
		f.hasCall = s.noteMemoryInstruction(op, depth, loopDepth, f.prevOp, f.prevIndex, &imm) || f.hasCall
	case 0xfb, 0xfe:
		if err := s.r.Reader.JumpTo(start + 1); err != nil {
			v.failed = true
			return
		}
		calls, err := s.scanMemoryInstruction(op, depth, loopDepth, f.prevOp, f.prevIndex)
		if err != nil || s.r.off() != end {
			v.failed = true
			return
		}
		f.hasCall = calls || f.hasCall
	case 0x08, 0x0a:
		s.h.flags.set(hintModuleEH)
	default:
		if _, ok := wasm.ImmediateFreeInstructionKind(op); ok {
			if s.h.immediateFreeOpCount() < defaultStackArenaCap {
				s.h.immediateFreeOps++
			}
		} else {
			if err := s.r.Reader.JumpTo(start + 1); err != nil {
				v.failed = true
				return
			}
			if err := s.scanOtherInstruction(op); err != nil || s.r.off() != end {
				v.failed = true
				return
			}
		}
	}
	f.prevPrevOp, f.prevPrevIndex = f.prevOp, f.prevIndex
	f.prevOp, f.prevIndex = op, currentIndex
}
