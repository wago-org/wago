//go:build amd64

package amd64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func funcHintStorageBytes(hints []funcHints, sidecar funcHintSidecar) (headers, sidecars uint64) {
	headers = uint64(cap(hints)) * uint64(unsafe.Sizeof(funcHints{}))
	sidecars = uint64(cap(sidecar.localScore)+cap(sidecar.localLastGet))*uint64(unsafe.Sizeof(uint32(0))) +
		uint64(cap(sidecar.sparseGlobals))*uint64(unsafe.Sizeof(shared.GlobalHint{}))
	return
}

// Function pre-scan (OPTIMIZATIONS.md "FuncHints"): one allocation-conscious
// walk collects call/memory shape and loop-weighted hotness scores for register
// pinning. DecodeModule keeps only Func.BodyBytes, so normal decoded modules use
// the byte scanner; programmatically constructed modules that supply decoded
// instructions use the AST scanner.

const (
	loopWeightFactor   = 10
	maxLoopWeightDepth = 6
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

type funcHintFlags uint16

const (
	hintHasCall funcHintFlags = 1 << iota
	hintHasTailCall
	hintCallsSelf
	hintTouchesMemory
	hintUsesBulkMem
	hintMutatesTable
	hintGCSharedResolver
	hintHasLoop
	hintHasJumpTableData
	hintHasControlFlow
	hintModuleEH
	hintHasFloatConst
	hintHasSIMD
	hintHasStackSinkFusion
	hintGCDeferredResolver
	hintIntervalRegionStorage
)

func (f funcHintFlags) has(flag funcHintFlags) bool { return f&flag != 0 }

func (f *funcHintFlags) set(flag funcHintFlags) { *f |= flag }

func (f *funcHintFlags) assign(flag funcHintFlags, value bool) {
	if value {
		*f |= flag
	} else {
		*f &^= flag
	}
}

// funcHints is everything scanFuncBody yields.
type funcHints struct {
	// gcResolverAndRelocs packs a saturated 24-bit conservative GC resolver-site
	// count plus an 8-bit outgoing local-call relocation reservation hint.
	gcResolverAndRelocs uint32
	localStart          uint32
	lastGetStartPlus1   uint32 // zero when interval-region side storage was not retained
	globalStart         uint32
	globalCount         uint32

	// stackArenaNodes is a conservative pre-scan estimate of operand-stack elem
	// allocations while compiling this body. It lets compileFunc avoid reserving
	// arena nodes for long immediates (notably v128.const payload bytes) while the
	// stack's heap fallback still preserves pointer stability if the estimate is
	// low for unusual control flow.
	stackArenaNodes uint32
	// stackArenaDiscount packs a 15-bit bounded discount plus the deep variable-
	// shift pressure flag. The prediction stays in existing storage
	// rather than growing the 64-byte retained function header.
	stackArenaDiscount uint16
	localCount         uint16 // complete parameter-plus-declared-local population
	// flags retain exact call, memory, control, inline-candidacy, and scan-gating
	// facts in the word defined above; no fact is reconstructed heuristically.
	flags funcHintFlags
	// maxControlDepth is the greatest simultaneously open structured-control
	// depth, excluding the implicit function frame. Byte-backed production
	// modules populate it during the existing one-pass hint scan. The byte uses
	// existing alignment padding; 255 is a saturated fallback sentinel.
	maxControlDepth uint8
	// inlineCallSites packs a saturated 7-bit ordinary-call count plus a high
	// bit recording any return_call reference to this local function.
	inlineCallSites uint8
}

const gcResolverSiteMask = uint32(1<<24 - 1)

func (h *funcHints) addGCResolverSite() {
	if h.gcResolverAndRelocs&gcResolverSiteMask != gcResolverSiteMask {
		h.gcResolverAndRelocs++
	}
}

func (h *funcHints) addCallRelocSite() {
	if h.gcResolverAndRelocs>>24 != ^uint32(0)>>24 {
		h.gcResolverAndRelocs += 1 << 24
	}
}

func (h funcHints) gcResolverSiteCount() uint32 { return h.gcResolverAndRelocs & gcResolverSiteMask }
func (h funcHints) callRelocSiteCount() uint8   { return uint8(h.gcResolverAndRelocs >> 24) }

const (
	stackArenaDiscountMask = uint16(1<<15 - 1)
	deepVariableShiftFlag  = uint16(1 << 15)
)

// funcHintView reconstructs scan/compile slices on the stack. Only funcHints is
// retained per function; all variable-length data lives in one module sidecar.
type funcHintView struct {
	funcHints
	entryInitialized uint64 // scan-local view; compilation decodes bits during pin planning
	nLocals          int
	localScore       []uint32
	localLastGet     []uint32
	sparseGlobals    []shared.GlobalHint
}

type funcHintSidecar struct {
	localScore    []uint32
	localLastGet  []uint32
	sparseGlobals []shared.GlobalHint
}

func retainedLocalScoreCount(h funcHints) int {
	n := int(h.localCount)
	if n > 64 && !h.flags.has(hintIntervalRegionStorage) {
		return 64
	}
	return n
}

func (s funcHintSidecar) view(h funcHints) funcHintView {
	nLocals := int(h.localCount)
	localStart := int(h.localStart)
	localEnd := localStart + retainedLocalScoreCount(h)
	var localLastGet []uint32
	if h.lastGetStartPlus1 != 0 {
		lastGetStart := int(h.lastGetStartPlus1 - 1)
		localLastGet = s.localLastGet[lastGetStart : lastGetStart+nLocals]
	}
	globalStart := int(h.globalStart)
	globalEnd := globalStart + int(h.globalCount)
	return funcHintView{
		funcHints:     h,
		nLocals:       nLocals,
		localScore:    s.localScore[localStart:localEnd],
		localLastGet:  localLastGet,
		sparseGlobals: s.sparseGlobals[globalStart:globalEnd],
	}
}

func (h *funcHints) addStackArenaNodes(n uint32) {
	if !shared.StackArenaHintsEnabled {
		return
	}
	if ^uint32(0)-h.stackArenaNodes < n {
		h.stackArenaNodes = ^uint32(0)
	} else {
		h.stackArenaNodes += n
	}
}

func (h *funcHints) addStackArenaDiscount(n uint16) {
	if !shared.StackArenaHintsEnabled {
		return
	}
	flags := h.stackArenaDiscount &^ stackArenaDiscountMask
	discount := h.stackArenaDiscount & stackArenaDiscountMask
	if stackArenaDiscountMask-discount < n {
		discount = stackArenaDiscountMask
	} else {
		discount += n
	}
	h.stackArenaDiscount = flags | discount
}

func (h *funcHints) noteDeepVariableShift() { h.stackArenaDiscount |= deepVariableShiftFlag }

func (h funcHints) hasDeepVariableShift() bool {
	return h.stackArenaDiscount&deepVariableShiftFlag != 0
}

func (h funcHints) arenaDiscount() uint16 { return h.stackArenaDiscount & stackArenaDiscountMask }

func (h *funcHints) noteControlDepth(depth int) {
	if depth >= 255 {
		h.maxControlDepth = 255
	} else if d := uint8(depth); d > h.maxControlDepth {
		h.maxControlDepth = d
	}
}

func newFuncHints(nLocals, nGlobals int) funcHintView {
	h := funcHintsWithStorage(make([]uint32, nLocals))
	h.localLastGet = make([]uint32, nLocals)
	h.localCount = uint16(nLocals)
	h.nLocals = nLocals
	return h
}

func funcHintsWithStorage(localScore []uint32) funcHintView {
	return funcHintView{nLocals: len(localScore), localScore: localScore}
}

const (
	localScoreEntryInitialized = uint32(1 << 31)
	localScoreHotnessMask      = localScoreEntryInitialized - 1
)

func localHotness(score uint32) uint32 { return score & localScoreHotnessMask }

func (h *funcHintView) markEntryInitialized(idx uint32) {
	if idx >= 64 || int(idx) >= len(h.localScore) {
		return
	}
	h.entryInitialized |= uint64(1) << idx
	h.localScore[idx] |= localScoreEntryInitialized
}

func finishGlobalHints(h funcHintView, accum *shared.GlobalHintAccumulator) funcHintView {
	h.sparseGlobals = accum.AppendTo(h.sparseGlobals[:0])
	h.globalCount = uint32(len(h.sparseGlobals))
	return h
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

type immutableTableHint struct {
	local             bool
	typeKey           uint64
	typed             bool
	monomorphicTarget int // local function index when every non-null entry is identical; -1 otherwise
}

type globalEligibilityTracker struct {
	marks   []uint32
	epoch   uint32
	globals []uint32
	frames  []globalEligibilityFrame
}

type globalEligibilityFrame struct {
	start int
	epoch uint32
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

// scanFuncBody chooses the byte-backed scanner used for decoded modules, falling
// back to the AST scanner for tests or callers that construct Func.Body directly.
func scanFuncBody(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32) (funcHintView, error) {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	var accum shared.GlobalHintAccumulator
	accum.Reset(nGlobals)
	h, err := scanFuncBodyIntoMemory64WithModuleCalls(fn, nLocals, nGlobals, selfIdx, h, &elig, false, nil, nil, nil, false, nil, 0, &accum)
	return finishGlobalHints(h, &accum), err
}

func scanFuncBodyIntoMemory64WithModuleCalls(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, h funcHintView, elig *globalEligibilityTracker, memory64 bool, m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool, moduleHints []funcHints, importedFuncs int, globalHints *shared.GlobalHintAccumulator) (funcHintView, error) {
	if len(fn.BodyBytes) != 0 {
		return scanBodyBytesIntoMemory64WithModuleCalls(fn.BodyBytes, nLocals, nGlobals, selfIdx, h, elig, memory64, m, classifier, gcTypeLayouts, gcStructHelpers, moduleHints, nil, importedFuncs, globalHints)
	}
	return scanBodyInto(fn.Body, nLocals, nGlobals, selfIdx, h, elig, m, gcTypeLayouts, gcStructHelpers, globalHints), nil
}

func gcOrAtomicInstructionMayCall(kind wasm.InstrKind, gcStructHelpers bool) bool {
	switch kind {
	case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructNewDesc, wasm.InstrStructNewDefaultDesc,
		wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructAtomicGet, wasm.InstrStructAtomicGetS, wasm.InstrStructAtomicGetU, wasm.InstrStructSet,
		wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData, wasm.InstrArrayNewElem,
		wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen,
		wasm.InstrArrayFill, wasm.InstrArrayCopy, wasm.InstrArrayInitData, wasm.InstrArrayInitElem,
		wasm.InstrMemoryAtomicNotify, wasm.InstrMemoryAtomicWait32, wasm.InstrMemoryAtomicWait64:
		return true
	case wasm.InstrRefGetDesc, wasm.InstrRefTest, wasm.InstrRefCast, wasm.InstrRefTestDesc, wasm.InstrRefCastDescEq, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail,
		wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny, wasm.InstrRefI31, wasm.InstrI31GetS, wasm.InstrI31GetU:
		return gcStructHelpers
	default:
		return false
	}
}

func directGCResolverInstruction(m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, kind wasm.InstrKind, typeIndex, fieldIndex uint32) bool {
	if m == nil {
		switch kind {
		case wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet,
			wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen:
			return true
		default:
			return false
		}
	}
	switch kind {
	case wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet:
		if int(typeIndex) < len(gcTypeLayouts) {
			layout := gcTypeLayouts[typeIndex]
			if layout.Type != nil && layout.Type.Comp.Kind == wasm.CompStruct && int(fieldIndex) < len(layout.FieldLayout) {
				_, ok := directGCScalarStorage(layout.Type.Comp.Fields[fieldIndex].Storage())
				return ok && layout.Type.Final
			}
		}
		_, _, final, ok := directGCStructLayout(m, typeIndex, fieldIndex)
		return ok && final
	case wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet:
		if int(typeIndex) < len(gcTypeLayouts) {
			layout := gcTypeLayouts[typeIndex]
			if layout.Type != nil && layout.Type.Comp.Kind == wasm.CompArray {
				_, ok := directGCScalarStorage(layout.Type.Comp.Array.Storage())
				return ok && layout.Type.Final
			}
		}
		_, final, ok := directGCArrayLayout(m, typeIndex)
		return ok && final
	case wasm.InstrArrayLen:
		// array.len has no type immediate. Exact-local dataflow may still select the
		// direct path, but counting it here can turn one real site plus one abstract
		// helper site into a code-growing shared island. Keep the crossover strict.
		return false
	default:
		return false
	}
}

// scanBody performs the AST pre-scan walk. selfIdx is the function's global
// function index (for callsSelf).
func scanBody(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32) funcHintView {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	var accum shared.GlobalHintAccumulator
	accum.Reset(nGlobals)
	return finishGlobalHints(scanBodyInto(body, nLocals, nGlobals, selfIdx, h, &elig, nil, nil, false, &accum), &accum)
}

func scanBodyInto(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32, h funcHintView, elig *globalEligibilityTracker, m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool, globalHints *shared.GlobalHintAccumulator) funcHintView {
	elig.reset()
	// Programmatic decoded bodies are uncommon and can exercise context-sensitive
	// folds that the production byte scan accounts for. Keep their legacy arena.
	if shared.StackArenaHintsEnabled {
		h.flags.set(hintHasStackSinkFusion)
	}
	// walk returns whether the subtree contains a call. curLoop identifies the
	// innermost enclosing loop whose globals are being considered for eligibility.
	var walk func(instrs []wasm.Instruction, depth int, curLoop int) bool
	walk = func(instrs []wasm.Instruction, depth int, curLoop int) bool {
		w := loopWeight(depth)
		sub := false
		for i := range instrs {
			in := &instrs[i]
			if in.Kind == wasm.InstrF32Const || in.Kind == wasm.InstrF64Const {
				h.flags.set(hintHasFloatConst)
			}
			if wasm.IsSIMDValidationInstructionKind(in.Kind) {
				h.flags.set(hintHasSIMD)
			}
			if shared.InstructionNeedsEHFrame(0, in.Kind) {
				h.flags.set(hintModuleEH)
			}
			if gcOrAtomicInstructionMayCall(in.Kind, gcStructHelpers) {
				h.flags.set(hintHasCall)
				sub = true
			}
			if shared.InstructionNeedsInlineBoundary(0, in.Kind) {
				h.flags.set(hintHasControlFlow)
				if in.Kind == wasm.InstrLoop {
					h.flags.set(hintHasLoop)
				} else if in.Kind == wasm.InstrBrTable && len(in.Indices()) >= brTableJumpMin {
					h.flags.set(hintHasJumpTableData)
				}
			}
			switch in.Kind {
			case wasm.InstrCall, wasm.InstrReturnCall, wasm.InstrCallRef, wasm.InstrReturnCallRef:
				sub = true
				h.flags.set(hintHasCall)
				if in.Kind == wasm.InstrReturnCall || in.Kind == wasm.InstrReturnCallRef {
					h.flags.set(hintHasTailCall)
				}
				if in.Kind == wasm.InstrCall && in.Index == selfIdx {
					h.flags.set(hintCallsSelf)
				}
			case wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect:
				sub = true
				h.flags.set(hintHasCall)
				if in.Kind == wasm.InstrReturnCallIndirect {
					h.flags.set(hintHasTailCall)
				}
			case wasm.InstrLocalGet:
				if int(in.Index) < nLocals {
					addHotness(h.localScore, in.Index, w)
				}
			case wasm.InstrLocalSet, wasm.InstrLocalTee:
				if int(in.Index) < nLocals {
					addHotness(h.localScore, in.Index, 2*w)
				}
			case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
				if int(in.Index) < nGlobals {
					if in.Kind == wasm.InstrGlobalSet {
						addGlobalHotness(globalHints, in.Index, 2*w)
					} else {
						addGlobalHotness(globalHints, in.Index, w)
					}
					elig.add(curLoop, in.Index)
				}
			case wasm.InstrLoop:
				loop := elig.push()
				if walk(in.Body().Instrs, depth+1, loop) {
					sub = true // call inside: its globals are not eligible
				} else {
					for _, g := range elig.globalsIn(loop) {
						markGlobalEligible(globalHints, g)
					}
				}
				elig.pop(loop)
			case wasm.InstrBlock, wasm.InstrTryTable:
				if walk(in.Body().Instrs, depth, curLoop) {
					sub = true
				}
			case wasm.InstrIf:
				if walk(in.Then(), depth, curLoop) {
					sub = true
				}
				if walk(in.Else(), depth, curLoop) {
					sub = true
				}
			case wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
				h.flags.set(hintUsesBulkMem | hintTouchesMemory)
			case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet:
				if directGCResolverInstruction(m, gcTypeLayouts, in.Kind, in.Index, in.Index2) {
					h.addGCResolverSite()
				}
			case wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen:
				if directGCResolverInstruction(m, gcTypeLayouts, in.Kind, in.Index, in.Index2) {
					h.addGCResolverSite()
				}
			case wasm.InstrTableSet, wasm.InstrTableInit, wasm.InstrTableCopy,
				wasm.InstrTableGrow, wasm.InstrTableFill:
				h.flags.set(hintMutatesTable)
			default:
				if instrTouchesMemory(in.Kind) {
					h.flags.set(hintTouchesMemory)
				}
			}
		}
		return sub
	}
	walk(body.Instrs, 0, -1)
	return h
}

func scanFuncGlobalScores(m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, fn wasm.Func, nGlobals int, add func(g uint32, score int64)) error {
	if len(fn.BodyBytes) != 0 {
		return scanBodyBytesGlobalScores(m, classifier, fn.BodyBytes, nGlobals, add)
	}
	scanBodyGlobalScores(fn.Body, nGlobals, add)
	return nil
}

func scanBodyGlobalScores(body wasm.Expr, nGlobals int, add func(g uint32, score int64)) {
	var walk func(instrs []wasm.Instruction, depth int)
	walk = func(instrs []wasm.Instruction, depth int) {
		w := loopWeight(depth)
		for i := range instrs {
			in := &instrs[i]
			switch in.Kind {
			case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
				if int(in.Index) < nGlobals {
					score := w
					if in.Kind == wasm.InstrGlobalSet {
						score = 2 * w
					}
					add(in.Index, score)
				}
			case wasm.InstrLoop:
				walk(in.Body().Instrs, depth+1)
			case wasm.InstrBlock, wasm.InstrTryTable:
				walk(in.Body().Instrs, depth)
			case wasm.InstrIf:
				walk(in.Then(), depth)
				walk(in.Else(), depth)
			}
		}
	}
	walk(body.Instrs, 0)
}

func scanBodyBytesGlobalScores(m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, body []byte, nGlobals int, add func(g uint32, score int64)) error {
	r := wasm.ReaderFrom(body)
	var cached wasm.ModuleInstructionClassifier
	if classifier != nil {
		cached = *classifier
	} else {
		cached = wasm.NewModuleInstructionClassifier(m, true)
	}
	s := globalScoreByteScanner{r: byteScanReader{Reader: r}, nGlobals: nGlobals, add: add, m: m, classifier: cached}
	term, err := s.scanExpr(0, 0, false)
	if err != nil {
		return err
	}
	if term != 0x0b || s.r.has() {
		return s.r.err(wasm.ErrInvalidInstruction, s.r.off())
	}
	return nil
}

type globalScoreByteScanner struct {
	r          byteScanReader
	nGlobals   int
	add        func(g uint32, score int64)
	m          *wasm.Module
	classifier wasm.ModuleInstructionClassifier
}

func (s *globalScoreByteScanner) scanExpr(depth int, loopDepth int, stopAtElse bool) (byte, error) {
	if depth > 20000 {
		return 0, s.r.err(wasm.ErrInstructionNestingLimitExceeded, s.r.off())
	}
	var imm wasm.InstructionImmediate
	for {
		op, err := s.r.byte()
		if err != nil {
			return 0, err
		}
		switch op {
		case 0x0b: // end
			return op, nil
		case 0x05: // else
			if stopAtElse {
				return op, nil
			}
			return op, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
		case 0x02, 0x03, 0x04: // block, loop, if
			if err := s.classifyInstructionInto(op, &imm); err != nil {
				return 0, err
			}
			switch op {
			case 0x02: // block
				term, err := s.scanExpr(depth+1, loopDepth, false)
				if err != nil {
					return 0, err
				}
				if term != 0x0b {
					return term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
			case 0x03: // loop
				term, err := s.scanExpr(depth+1, loopDepth+1, false)
				if err != nil {
					return 0, err
				}
				if term != 0x0b {
					return term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
			case 0x04: // if
				term, err := s.scanExpr(depth+1, loopDepth, true)
				if err != nil {
					return 0, err
				}
				if term == 0x05 {
					term, err = s.scanExpr(depth+1, loopDepth, false)
					if err != nil {
						return 0, err
					}
				}
				if term != 0x0b {
					return term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
			}
		case 0x23, 0x24: // global.get/set
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return 0, err
			}
			idx := imm.Index
			if int(idx) < s.nGlobals {
				score := loopWeight(loopDepth)
				if op == 0x24 {
					score *= 2
				}
				s.add(idx, score)
			}
		case 0x1f: // try_table: blocktype, catch vector, body
			if err := s.classifyInstructionInto(op, &imm); err != nil {
				return 0, err
			}
			term, err := s.scanExpr(depth+1, loopDepth, false)
			if err != nil {
				return 0, err
			}
			if term != 0x0b {
				return term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
			}
		default:
			if err := s.classifyInstructionInto(op, &imm); err != nil {
				return 0, err
			}
		}
	}
}

func (s *globalScoreByteScanner) classifyInstructionInto(op byte, imm *wasm.InstructionImmediate) error {
	if s.m != nil {
		return s.classifier.ClassifyInto(&s.r.Reader, op, imm)
	}
	return wasm.ClassifyInstructionImmediateInto(&s.r.Reader, op, imm)
}

// scanBodyBytes performs the same pre-scan over raw expression bytecode without
// allocating Instruction trees. body includes the terminating end opcode and
// excludes local declarations.
func scanBodyBytes(body []byte, nLocals int, nGlobals int, selfIdx uint32) (funcHintView, error) {
	return scanBodyBytesMemory64(body, nLocals, nGlobals, selfIdx, false)
}

func scanBodyBytesMemory64(body []byte, nLocals int, nGlobals int, selfIdx uint32, memory64 bool) (funcHintView, error) {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	var accum shared.GlobalHintAccumulator
	accum.Reset(nGlobals)
	h, err := scanBodyBytesIntoMemory64WithModuleCalls(body, nLocals, nGlobals, selfIdx, h, &elig, memory64, nil, nil, nil, false, nil, nil, 0, &accum)
	return finishGlobalHints(h, &accum), err
}

func scanBodyBytesIntoMemory64WithModuleCalls(body []byte, nLocals int, nGlobals int, selfIdx uint32, h funcHintView, elig *globalEligibilityTracker, memory64 bool, m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool, moduleHints []funcHints, parallelCalls []parallelCalleeHints, importedFuncs int, globalHints *shared.GlobalHintAccumulator) (funcHintView, error) {
	elig.reset()
	r := wasm.ReaderFrom(body)
	var cached wasm.ModuleInstructionClassifier
	if classifier != nil {
		cached = *classifier
	} else {
		cached = wasm.NewModuleInstructionClassifier(m, true)
	}
	s := byteBodyScanner{r: byteScanReader{Reader: r}, h: h, nLocals: nLocals, nGlobals: nGlobals, selfIdx: selfIdx, elig: elig, globalHints: globalHints, entryPrefix: true, memory64: memory64, m: m, classifier: cached, gcTypeLayouts: gcTypeLayouts, gcStructHelpers: gcStructHelpers, moduleHints: moduleHints, parallelCalls: parallelCalls, importedFuncs: importedFuncs}
	called, term, err := s.scanExpr(0, 0, -1, false)
	if err != nil {
		return s.h, err
	}
	if called {
		s.h.flags.set(hintHasCall)
	}
	if term != 0x0b || s.r.has() {
		return s.h, s.r.err(wasm.ErrInvalidInstruction, s.r.off())
	}
	return s.h, nil
}

type byteBodyScanner struct {
	r           byteScanReader
	h           funcHintView
	nLocals     int
	nGlobals    int
	selfIdx     uint32
	elig        *globalEligibilityTracker
	globalHints *shared.GlobalHintAccumulator

	entryPrefix     bool
	entrySeen       uint64
	memory64        bool
	m               *wasm.Module
	classifier      wasm.ModuleInstructionClassifier
	gcTypeLayouts   []codegen.GCTypeLayout
	gcStructHelpers bool
	moduleHints     []funcHints
	parallelCalls   []parallelCalleeHints
	importedFuncs   int
}

func (s *byteBodyScanner) scanExpr(depth int, loopDepth int, curLoop int, stopAtElse bool) (bool, byte, error) {
	if depth > 20000 {
		return true, 0, s.r.err(wasm.ErrInstructionNestingLimitExceeded, s.r.off())
	}
	subHasCall := false
	var prevOp, prevPrevOp byte
	var prevIndex, prevPrevIndex uint32
	var prevConst int64
	var prevConstOK bool
	var variableShiftDepth uint8
	for {
		op, err := s.r.byte()
		if err != nil {
			return true, 0, err
		}
		curIndex := ^uint32(0)
		var curConst int64
		curConstOK := false
		if shared.StackArenaHintsEnabled && (op == 0x41 || op == 0x42) {
			if b, ok := s.r.Peek(); ok && b&0x80 == 0 {
				curConst = int64(b & 0x7f)
				if b&0x40 != 0 {
					curConst |= ^int64(0x7f)
				}
				curConstOK = true
			}
		}
		if op == 0x43 || op == 0x44 {
			s.h.flags.set(hintHasFloatConst)
		} else if op == 0xfd {
			s.h.flags.set(hintHasSIMD)
		}
		if shared.InstructionNeedsEHFrame(op, wasm.InstrInvalid) {
			s.h.flags.set(hintModuleEH)
		}
		if shared.InstructionNeedsInlineBoundary(op, wasm.InstrInvalid) {
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
			if op == 0x03 {
				s.h.flags.set(hintHasLoop)
			}
		}
		switch op {
		case 0x0b: // end
			s.h.addStackArenaNodes(2) // flush/rebuild allowance for the closing edge.
			return subHasCall, op, nil
		case 0x05: // else
			s.h.addStackArenaNodes(2) // then-edge flush plus else-entry rebuild.
			if stopAtElse {
				return subHasCall, op, nil
			}
			return true, op, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
		case 0x0e: // br_table: exact jump-table-data admission hint
			n, err := s.r.U32()
			if err != nil {
				return true, 0, err
			}
			if n >= brTableJumpMin {
				s.h.flags.set(hintHasJumpTableData)
			}
			for i := uint32(0); i < n; i++ {
				if _, err := s.r.U32(); err != nil {
					return true, 0, err
				}
			}
			if _, err := s.r.U32(); err != nil { // default label
				return true, 0, err
			}
		case 0x02, 0x03, 0x04: // block, loop, if
			s.h.addStackArenaNodes(2) // entry flush/rebuild allowance.
			s.h.noteControlDepth(depth + 1)
			if err := wasm.SkipInstructionImmediate(&s.r.Reader, op); err != nil {
				return true, 0, err
			}
			switch op {
			case 0x02: // block
				calls, term, err := s.scanExpr(depth+1, loopDepth, curLoop, false)
				if err != nil {
					return true, 0, err
				}
				if term != 0x0b {
					return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
				subHasCall = subHasCall || calls
			case 0x03: // loop
				loop := s.elig.push()
				calls, term, err := s.scanExpr(depth+1, loopDepth+1, loop, false)
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
				callsThen, term, err := s.scanExpr(depth+1, loopDepth, curLoop, true)
				if err != nil {
					return true, 0, err
				}
				callsElse := false
				if term == 0x05 {
					callsElse, term, err = s.scanExpr(depth+1, loopDepth, curLoop, false)
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
			if shared.StackArenaHintsEnabled {
				s.h.addStackArenaNodes(1)
			}
			s.h.flags.set(hintHasCall)
			subHasCall = true
			if op == 0x12 {
				s.h.flags.set(hintHasTailCall)
			}
			if op == 0x10 && idx == s.selfIdx {
				s.h.flags.set(hintCallsSelf)
			}
			s.noteDirectCallRef(idx, op == 0x10)
		case 0x11, 0x13: // indirect calls
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			s.h.flags.set(hintHasCall)
			subHasCall = true
			if op == 0x13 || op == 0x15 {
				s.h.flags.set(hintHasTailCall)
			}
		case 0x14, 0x15: // call_ref, return_call_ref
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
			if shared.StackArenaHintsEnabled {
				s.h.addStackArenaNodes(1)
			}
			s.h.flags.set(hintHasCall)
			subHasCall = true
			if op == 0x15 {
				s.h.flags.set(hintHasTailCall)
			}
		case 0x20, 0x21, 0x22: // local.get/set/tee
			idx, err := s.r.U32()
			if err != nil {
				return true, 0, err
			}
			if shared.StackArenaHintsEnabled && op == 0x20 {
				s.h.addStackArenaNodes(1)
			}
			if op == 0x22 {
				s.h.addStackArenaDiscount(stackLookaheadDiscountOpcode(prevOp))
			}
			curIndex = idx
			if int(idx) < s.nLocals {
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
					addHotness(s.h.localScore, idx, loopWeight(loopDepth))
					if int(idx) < len(s.h.localLastGet) {
						s.h.localLastGet[idx] = uint32(s.r.off())
					}
				} else {
					addHotness(s.h.localScore, idx, 2*loopWeight(loopDepth))
				}
			}
		case 0x23, 0x24: // global.get/set
			idx, err := s.r.U32()
			if err != nil {
				return true, 0, err
			}
			if shared.StackArenaHintsEnabled && op == 0x23 {
				s.h.addStackArenaNodes(1)
			}
			if int(idx) < s.nGlobals {
				if op == 0x24 {
					addGlobalHotness(s.globalHints, idx, 2*loopWeight(loopDepth))
				} else {
					addGlobalHotness(s.globalHints, idx, loopWeight(loopDepth))
				}
				s.elig.add(curLoop, idx)
			}
		case 0x41: // i32.const
			if _, err := s.r.I32(); err != nil {
				return true, 0, err
			}
			if shared.StackArenaHintsEnabled {
				s.h.addStackArenaNodes(1)
			}
		case 0x42: // i64.const
			if _, err := s.r.I64(); err != nil {
				return true, 0, err
			}
			if shared.StackArenaHintsEnabled {
				s.h.addStackArenaNodes(1)
			}
		case 0x43: // f32.const
			if _, err := s.r.Bytes(4); err != nil {
				return true, 0, err
			}
			if shared.StackArenaHintsEnabled {
				s.h.addStackArenaNodes(1)
			}
		case 0x44: // f64.const
			if _, err := s.r.Bytes(8); err != nil {
				return true, 0, err
			}
			if shared.StackArenaHintsEnabled {
				s.h.addStackArenaNodes(1)
			}
		case 0x0c, 0x0d: // br, br_if
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
		case 0x25, 0x26: // table.get/set
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
			if op == 0x25 && shared.StackArenaHintsEnabled {
				s.h.addStackArenaNodes(1)
			} else if op == 0x26 {
				s.h.flags.set(hintMutatesTable)
			}
		case 0xd2, 0xd5, 0xd6: // ref.func, br_on_null, br_on_non_null
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
			if shared.StackArenaHintsEnabled {
				s.h.addStackArenaNodes(1)
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0xfc, 0xfd, 0xfe, 0xfb:
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
				s.h.flags.set(hintHasControlFlow)
			}
			if shared.InstructionNeedsEHFrame(op, imm.Kind) {
				s.h.flags.set(hintModuleEH)
			}
			if gcOrAtomicInstructionMayCall(imm.Kind, s.gcStructHelpers) {
				s.h.flags.set(hintHasCall)
				subHasCall = true
			}
			if op == 0xfb {
				switch imm.Kind {
				case wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet,
					wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen:
					if directGCResolverInstruction(s.m, s.gcTypeLayouts, imm.Kind, imm.Index, imm.Index2) {
						s.h.addGCResolverSite()
					}
				}
			}
			if imm.TouchesMemory {
				s.h.flags.set(hintTouchesMemory)
			}
			if imm.UsesBulkMemory {
				s.h.flags.set(hintUsesBulkMem)
			}
		case 0x1f: // try_table: blocktype, catch vector, body
			s.h.addStackArenaNodes(2) // entry flush/rebuild allowance.
			s.h.noteControlDepth(depth + 1)
			if err := wasm.SkipInstructionImmediate(&s.r.Reader, op); err != nil {
				return true, 0, err
			}
			calls, term, err := s.scanExpr(depth+1, loopDepth, curLoop, false)
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
				s.noteImmediateFreeStackArenaOp(op)
				break
			}
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
				s.h.flags.set(hintHasControlFlow)
			}
			if shared.InstructionNeedsEHFrame(op, imm.Kind) {
				s.h.flags.set(hintModuleEH)
			}
			if imm.TouchesMemory {
				s.h.flags.set(hintTouchesMemory)
			}
			if imm.UsesBulkMemory {
				s.h.flags.set(hintUsesBulkMem)
			}
		}
		if variableShiftOpcode(op) && prevOp != 0x41 && prevOp != 0x42 {
			if variableShiftDepth < maxDeferDepth {
				variableShiftDepth++
			}
			if variableShiftDepth >= maxDeferDepth {
				s.h.noteDeepVariableShift()
			}
		} else if op != 0x20 { // local.get may supply the next variable shift count.
			variableShiftDepth = 0
		}
		if shared.StackArenaHintsEnabled {
			if !prevConstOK && (prevOp == 0x41 || prevOp == 0x42) &&
				(op >= 0x6a && op <= 0x78 || op >= 0x7c && op <= 0x8a) {
				s.h.flags.set(hintHasStackSinkFusion)
			}
			if stackFlowTerminatorOpcode(op) {
				if next, ok := s.r.Peek(); ok && next != 0x0b && next != 0x05 {
					s.h.flags.set(hintHasStackSinkFusion)
				}
			}
			s.h.addStackArenaDiscount(stackAlgebraicDiscountOpcode(op, prevOp, prevPrevOp, prevIndex, prevPrevIndex, prevConst, prevConstOK))
			prevPrevOp, prevPrevIndex = prevOp, prevIndex
			prevOp, prevIndex = op, curIndex
			prevConst, prevConstOK = curConst, curConstOK
		}
	}
}

func variableShiftOpcode(op byte) bool {
	return op >= 0x74 && op <= 0x78 || op >= 0x86 && op <= 0x8a
}

func (s *byteBodyScanner) noteDirectCallRef(globalIdx uint32, inline bool) {
	local := int(globalIdx) - s.importedFuncs
	if local < 0 || local >= len(s.moduleHints) && local >= len(s.parallelCalls) {
		return
	}
	s.h.addCallRelocSite()
	if len(s.parallelCalls) != 0 {
		target := &s.parallelCalls[local]
		if inline {
			target.inline.Add(1)
		} else {
			target.tail.Store(true)
		}
		return
	}
	target := &s.moduleHints[local]
	if !inline {
		target.inlineCallSites |= 0x80
		return
	}
	count := target.inlineCallSites & 0x7f
	if count != 0x7f {
		target.inlineCallSites = target.inlineCallSites&0x80 | count + 1
	}
}

func (h funcHints) inlineCallSiteCount() uint8 { return h.inlineCallSites & 0x7f }

func (s *byteBodyScanner) classifyInstructionInto(op byte, imm *wasm.InstructionImmediate) error {
	var err error
	if s.m != nil {
		err = s.classifier.ClassifyInto(&s.r.Reader, op, imm)
	} else {
		err = wasm.ClassifyInstructionImmediateIntoWithFeatures(&s.r.Reader, op, imm, s.memory64, true)
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

func (s *byteBodyScanner) noteStackArenaOp(op byte, imm *wasm.InstructionImmediate) {
	if !shared.StackArenaHintsEnabled {
		return
	}
	if stackArenaOpAllocates(op, imm) {
		s.h.addStackArenaNodes(1)
	}
	if op == 0xfd {
		s.h.flags.set(hintHasStackSinkFusion)
	}
	if stackSinkFusionOpcode(op) {
		next, ok := s.r.Peek()
		if ok && (next == 0x21 || next == 0x22) {
			s.h.flags.set(hintHasStackSinkFusion)
		}
	}
}

func (s *byteBodyScanner) noteImmediateFreeStackArenaOp(op byte) {
	if !shared.StackArenaHintsEnabled {
		return
	}
	if op == 0x1b || op == 0xd1 || op >= 0x45 && op <= 0xc4 {
		s.h.addStackArenaNodes(1)
	}
	if stackSinkFusionOpcode(op) {
		next, ok := s.r.Peek()
		if ok && (next == 0x21 || next == 0x22) {
			s.h.flags.set(hintHasStackSinkFusion)
		}
	}
}

func stackSinkFusionOpcode(op byte) bool {
	return op == 0xfd || op >= 0x92 && op <= 0x95 || op >= 0xa0 && op <= 0xa3
}

func stackLookaheadDiscountOpcode(op byte) uint16 {
	switch op {
	case 0x83: // i64.and
		return 12
	case 0x88: // i64.shr_u
		return 32
	default:
		return 0
	}
}

func stackFlowTerminatorOpcode(op byte) bool {
	switch op {
	case 0x00, 0x08, 0x0a, 0x0c, 0x0e, 0x0f, 0x12, 0x13, 0x15:
		return true
	default:
		return false
	}
}

func stackAlgebraicDiscountOpcode(op, prevOp, prevPrevOp byte, prevIndex, prevPrevIndex uint32, prevConst int64, prevConstOK bool) uint16 {
	if prevConstOK && algebraicIdentityOpcode(op, prevOp, prevConst) || op == 0xa7 && prevOp == 0x84 {
		return 1
	}
	if prevOp == 0x20 && prevPrevOp == 0x20 && prevIndex == prevPrevIndex && sameOperandSimplifiesOpcode(op) {
		return 1
	}
	return 0
}

func algebraicIdentityOpcode(op, constOp byte, c int64) bool {
	if constOp != 0x41 && constOp != 0x42 {
		return false
	}
	switch op {
	case 0x6a, 0x6b, 0x72, 0x73, 0x7c, 0x7d, 0x84, 0x85:
		return c == 0
	case 0x74, 0x75, 0x76, 0x77, 0x78:
		return c&31 == 0
	case 0x86, 0x87, 0x88, 0x89, 0x8a:
		return c&63 == 0
	case 0x6c, 0x6e, 0x7e, 0x80:
		return c == 1
	case 0x71:
		return int32(c) == -1
	case 0x83:
		return c == -1
	default:
		return false
	}
}

func sameOperandSimplifiesOpcode(op byte) bool {
	if op >= 0x46 && op <= 0x4f || op >= 0x51 && op <= 0x5a {
		return true
	}
	switch op {
	case 0x6b, 0x71, 0x72, 0x73, 0x7d, 0x83, 0x84, 0x85:
		return true
	default:
		return false
	}
}

func stackArenaOpAllocates(op byte, imm *wasm.InstructionImmediate) bool {
	switch op {
	case 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, // calls: conservatively allow one result node.
		0x1b, 0x1c, // select
		0x20, 0x23, 0x25, // local.get/global.get/table.get
		0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, // loads
		0x3f, 0x40, // memory.size/grow
		0x41, 0x42, 0x43, 0x44, // constants
		0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
		0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
		0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66,
		0x67, 0x68, 0x69,
		0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		0x79, 0x7a, 0x7b,
		0x7c, 0x7d, 0x7e, 0x7f, 0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a,
		0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
		0x99, 0x9a, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f, 0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6,
		0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf, 0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf,
		0xc0, 0xc1, 0xc2, 0xc3, 0xc4,
		0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6:
		return true
	case 0xfc:
		return imm.Subopcode <= 7 || imm.Subopcode == 15 || imm.Subopcode == 16 // trunc_sat/table.grow/table.size push.
	case 0xfe:
		switch imm.Kind {
		case wasm.InstrAtomicFence,
			wasm.InstrI32AtomicStore, wasm.InstrI64AtomicStore,
			wasm.InstrI32AtomicStore8, wasm.InstrI32AtomicStore16,
			wasm.InstrI64AtomicStore8, wasm.InstrI64AtomicStore16, wasm.InstrI64AtomicStore32:
			return false
		default:
			return true // loads, wait/notify, RMW, and cmpxchg push one result.
		}
	case 0xfb:
		return true // GC/reference operations push at most one native result; result-free forms are safe overestimates.
	case 0xfd:
		switch imm.Subopcode {
		case 11, 88, 89, 90, 91: // v128.store and v128.store{8,16,32,64}_lane push no result.
			return false
		default:
			return true
		}
	default:
		return false
	}
}

type byteScanReader struct{ wasm.Reader }

func (r *byteScanReader) has() bool { return r.HasNext() }
func (r *byteScanReader) off() int  { return r.Offset() }
func (r *byteScanReader) err(code wasm.DecodeErrorCode, off int) error {
	return &wasm.DecodeError{Code: code, Offset: off}
}
func (r *byteScanReader) byte() (byte, error) { return r.Byte() }

func shouldSkipStackFence(hasCall bool, nLocalSlots int, bodyBytesLen int) bool {
	return !hasCall && frameHdrBytes+8*nLocalSlots+8*bodyBytesLen <= 4096
}

func instrTouchesMemory(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrI32Load, wasm.InstrI64Load, wasm.InstrF32Load, wasm.InstrF64Load,
		wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
		wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U,
		wasm.InstrI64Load32S, wasm.InstrI64Load32U,
		wasm.InstrI32Store, wasm.InstrI64Store, wasm.InstrF32Store, wasm.InstrF64Store,
		wasm.InstrI32Store8, wasm.InstrI32Store16, wasm.InstrI64Store8, wasm.InstrI64Store16,
		wasm.InstrI64Store32,
		wasm.InstrMemorySize, wasm.InstrMemoryGrow, wasm.InstrMemoryInit, wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
		return true
	default:
		return false
	}
}
