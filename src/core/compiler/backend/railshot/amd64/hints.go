//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

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

// funcHints is everything scanFuncBody yields.
type funcHints struct {
	nLocals       int
	hasCall       bool // any direct or indirect call
	hasTailCall   bool // any return_call/return_call_indirect/return_call_ref
	callsSelf     bool // a direct call to the function's own index
	touchesMemory bool // any linear-memory op
	usesBulkMem   bool // memory.copy/fill (rep movs/stos clobber RDI/RSI/RCX)
	mutatesTable  bool // table.set/init/copy/grow/fill; excludes immutable local-table call_indirect specialization
	// maxControlDepth is the greatest simultaneously open structured-control
	// depth, excluding the implicit function frame. Byte-backed production
	// modules populate it during the existing one-pass hint scan. The byte uses
	// existing alignment padding; 255 is a saturated fallback sentinel.
	maxControlDepth uint8
	// inlineCallSites packs a saturated 7-bit ordinary-call count plus a high
	// bit recording any return_call reference to this local function.
	inlineCallSites  uint8
	gcResolverSites  int  // conservative direct scalar/length resolver site count
	gcSharedResolver bool // module decision: shared island beats one-site inline crossover

	// Inline-candidacy signals, gathered in the same pre-scan so buildInlineTargets
	// needs no second body walk. hasControlFlow matches scanInlineFactsBytes's set
	// exactly (unreachable/block/loop/if/else/br*/return, NOT try_table); hasLoop is
	// the loop subset. calleeCount>0/hasControlCall from inlineOK both reduce to
	// hasCall (an inline candidate is leaf), so no separate call-kind split is kept.
	hasLoop            bool
	hasJumpTableData   bool
	hasControlFlow     bool
	moduleEH           bool
	hasFloatConst      bool // body contains f32.const or f64.const
	hasSIMD            bool // body contains an 0xfd SIMD instruction
	hasStackSinkFusion bool // a following local.set/tee may consume a scanned result without allocating it

	// immutableTables is derived after the one-pass per-function scans have been
	// aggregated (computeModuleHints). Each admitted table is local, unexported,
	// never mutated, and contains only same-module functions, so indirect calls may
	// use the internal register ABI without a run-time home/tag fork.
	immutableTables []immutableTableHint

	// Loop-weighted hotness: local.get/global.get = 1×, set/tee = 2×, ×loopWeight
	// per enclosing loop level.
	localScore  []uint32
	globalScore []uint32
	// localLastGet records the byte offset immediately after each local's final
	// local.get. It is filled by the production byte scanner and lets bounded
	// regional allocation release a cache register without another body walk.
	localLastGet []uint32
	// entryInitialized marks locals (up to 64) whose first access in the
	// function's straight-line entry prefix is local.set/tee. Their Wasm zero
	// value cannot be observed, so the prologue may skip initializing them.
	entryInitialized uint64

	// globalElig[g]: global g is accessed inside a loop whose subtree contains NO
	// call. Value-pinning such a global in a call-making function is a win: the
	// per-iteration memory traffic disappears while the coherence spill/reload
	// lands only on the (sparse) calls outside that loop. The innermost enclosing
	// loop decides — if it calls, no outer loop can be call-free.
	globalElig []bool
	// sparseGlobals replaces the dense score/eligibility slices for modules whose
	// functions-by-globals matrix would exceed the bounded dense fast path.
	sparseGlobals []shared.GlobalHint
	globalAccum   *shared.GlobalHintAccumulator

	// stackArenaNodes is a conservative pre-scan estimate of operand-stack elem
	// allocations while compiling this body. It lets compileFunc avoid reserving
	// arena nodes for long immediates (notably v128.const payload bytes) while the
	// stack's heap fallback still preserves pointer stability if the estimate is
	// low for unusual control flow.
	stackArenaNodes    uint32
	stackArenaDiscount uint16 // possible scanned nodes removed by bounded lookahead peepholes
}

func (h *funcHints) addStackArenaNodes(n uint32) {
	if ^uint32(0)-h.stackArenaNodes < n {
		h.stackArenaNodes = ^uint32(0)
	} else {
		h.stackArenaNodes += n
	}
}

func (h *funcHints) addStackArenaDiscount(n uint16) {
	if ^uint16(0)-h.stackArenaDiscount < n {
		h.stackArenaDiscount = ^uint16(0)
	} else {
		h.stackArenaDiscount += n
	}
}

func (h *funcHints) noteControlDepth(depth int) {
	if depth >= 255 {
		h.maxControlDepth = 255
	} else if d := uint8(depth); d > h.maxControlDepth {
		h.maxControlDepth = d
	}
}

func newFuncHints(nLocals, nGlobals int) funcHints {
	h := funcHintsWithStorage(make([]uint32, nLocals), make([]uint32, nGlobals), make([]bool, nGlobals))
	h.localLastGet = make([]uint32, nLocals)
	h.nLocals = nLocals
	return h
}

func funcHintsWithStorage(localScore, globalScore []uint32, globalElig []bool) funcHints {
	return funcHints{localScore: localScore, globalScore: globalScore, globalElig: globalElig}
}

func addHotness(scores []uint32, idx uint32, delta int64) {
	if int(idx) >= len(scores) || delta <= 0 {
		return
	}
	const max = ^uint32(0)
	if uint64(scores[idx])+uint64(delta) >= uint64(max) {
		scores[idx] = max
	} else {
		scores[idx] += uint32(delta)
	}
}

func (h *funcHints) addGlobalHotness(idx uint32, delta int64) {
	if h.globalAccum != nil {
		h.globalAccum.Add(idx, delta)
		return
	}
	addHotness(h.globalScore, idx, delta)
}

func (h *funcHints) markGlobalEligible(idx uint32) {
	if h.globalAccum != nil {
		h.globalAccum.MarkEligible(idx)
		return
	}
	if int(idx) < len(h.globalElig) {
		h.globalElig[idx] = true
	}
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
func scanFuncBody(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32) (funcHints, error) {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	return scanFuncBodyInto(fn, nLocals, nGlobals, selfIdx, h, &elig)
}

func scanFuncBodyInto(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, h funcHints, elig *globalEligibilityTracker) (funcHints, error) {
	return scanFuncBodyIntoMemory64(fn, nLocals, nGlobals, selfIdx, h, elig, false)
}

func scanFuncBodyIntoMemory64(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, h funcHints, elig *globalEligibilityTracker, memory64 bool) (funcHints, error) {
	return scanFuncBodyIntoMemory64WithModule(fn, nLocals, nGlobals, selfIdx, h, elig, memory64, nil, nil, false)
}

func scanFuncBodyIntoMemory64WithModule(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, h funcHints, elig *globalEligibilityTracker, memory64 bool, m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool) (funcHints, error) {
	return scanFuncBodyIntoMemory64WithModuleCalls(fn, nLocals, nGlobals, selfIdx, h, elig, memory64, m, nil, gcTypeLayouts, gcStructHelpers, nil, 0)
}

func scanFuncBodyIntoMemory64WithModuleCalls(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, h funcHints, elig *globalEligibilityTracker, memory64 bool, m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool, moduleHints []funcHints, importedFuncs int) (funcHints, error) {
	if len(fn.BodyBytes) != 0 {
		return scanBodyBytesIntoMemory64WithModuleCalls(fn.BodyBytes, nLocals, nGlobals, selfIdx, h, elig, memory64, m, classifier, gcTypeLayouts, gcStructHelpers, moduleHints, importedFuncs)
	}
	return scanBodyInto(fn.Body, nLocals, nGlobals, selfIdx, h, elig, m, gcTypeLayouts, gcStructHelpers), nil
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
func scanBody(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32) funcHints {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	return scanBodyInto(body, nLocals, nGlobals, selfIdx, h, &elig, nil, nil, false)
}

func scanBodyInto(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32, h funcHints, elig *globalEligibilityTracker, m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool) funcHints {
	elig.reset()
	// walk returns whether the subtree contains a call. curLoop identifies the
	// innermost enclosing loop whose globals are being considered for eligibility.
	var walk func(instrs []wasm.Instruction, depth int, curLoop int) bool
	walk = func(instrs []wasm.Instruction, depth int, curLoop int) bool {
		w := loopWeight(depth)
		sub := false
		for i := range instrs {
			in := &instrs[i]
			h.addStackArenaDiscount(stackAlgebraicDiscount(instrs, i))
			if wasm.IsSIMDValidationInstructionKind(in.Kind) {
				h.hasStackSinkFusion = true
			}
			if in.Kind == wasm.InstrLocalTee && i != 0 {
				h.addStackArenaDiscount(stackLookaheadDiscount(instrs[i-1].Kind))
			}
			if i+1 < len(instrs) && stackSinkFusionCandidate(in.Kind, instrs[i+1].Kind) {
				h.hasStackSinkFusion = true
			}
			if in.Kind == wasm.InstrF32Const || in.Kind == wasm.InstrF64Const {
				h.hasFloatConst = true
			}
			if wasm.IsSIMDValidationInstructionKind(in.Kind) {
				h.hasSIMD = true
			}
			if shared.InstructionNeedsEHFrame(0, in.Kind) {
				h.moduleEH = true
			}
			if gcOrAtomicInstructionMayCall(in.Kind, gcStructHelpers) {
				h.hasCall, sub = true, true
			}
			if shared.InstructionNeedsInlineBoundary(0, in.Kind) {
				h.hasControlFlow = true
				if in.Kind == wasm.InstrLoop {
					h.hasLoop = true
				} else if in.Kind == wasm.InstrBrTable && len(in.Indices()) >= brTableJumpMin {
					h.hasJumpTableData = true
				}
			}
			switch in.Kind {
			case wasm.InstrCall, wasm.InstrReturnCall, wasm.InstrCallRef, wasm.InstrReturnCallRef:
				sub, h.hasCall = true, true
				if in.Kind == wasm.InstrReturnCall || in.Kind == wasm.InstrReturnCallRef {
					h.hasTailCall = true
				}
				if in.Kind == wasm.InstrCall && in.Index == selfIdx {
					h.callsSelf = true
				}
			case wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect:
				sub, h.hasCall = true, true
				if in.Kind == wasm.InstrReturnCallIndirect {
					h.hasTailCall = true
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
						h.addGlobalHotness(in.Index, 2*w)
					} else {
						h.addGlobalHotness(in.Index, w)
					}
					elig.add(curLoop, in.Index)
				}
			case wasm.InstrLoop:
				loop := elig.push()
				if walk(in.Body().Instrs, depth+1, loop) {
					sub = true // call inside: its globals are not eligible
				} else {
					for _, g := range elig.globalsIn(loop) {
						h.markGlobalEligible(g)
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
				h.usesBulkMem, h.touchesMemory = true, true
			case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet:
				if directGCResolverInstruction(m, gcTypeLayouts, in.Kind, in.Index, in.Index2) {
					h.gcResolverSites++
				}
			case wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen:
				if directGCResolverInstruction(m, gcTypeLayouts, in.Kind, in.Index, in.Index2) {
					h.gcResolverSites++
				}
			case wasm.InstrTableSet, wasm.InstrTableInit, wasm.InstrTableCopy,
				wasm.InstrTableGrow, wasm.InstrTableFill:
				h.mutatesTable = true
			default:
				if instrTouchesMemory(in.Kind) {
					h.touchesMemory = true
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
func scanBodyBytes(body []byte, nLocals int, nGlobals int, selfIdx uint32) (funcHints, error) {
	return scanBodyBytesMemory64(body, nLocals, nGlobals, selfIdx, false)
}

func scanBodyBytesMemory64(body []byte, nLocals int, nGlobals int, selfIdx uint32, memory64 bool) (funcHints, error) {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	return scanBodyBytesIntoMemory64(body, nLocals, nGlobals, selfIdx, h, &elig, memory64)
}

func scanBodyBytesIntoMemory64(body []byte, nLocals int, nGlobals int, selfIdx uint32, h funcHints, elig *globalEligibilityTracker, memory64 bool) (funcHints, error) {
	return scanBodyBytesIntoMemory64WithModule(body, nLocals, nGlobals, selfIdx, h, elig, memory64, nil, nil, false)
}

func scanBodyBytesIntoMemory64WithModule(body []byte, nLocals int, nGlobals int, selfIdx uint32, h funcHints, elig *globalEligibilityTracker, memory64 bool, m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool) (funcHints, error) {
	return scanBodyBytesIntoMemory64WithModuleCalls(body, nLocals, nGlobals, selfIdx, h, elig, memory64, m, nil, gcTypeLayouts, gcStructHelpers, nil, 0)
}

func scanBodyBytesIntoMemory64WithModuleCalls(body []byte, nLocals int, nGlobals int, selfIdx uint32, h funcHints, elig *globalEligibilityTracker, memory64 bool, m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool, moduleHints []funcHints, importedFuncs int) (funcHints, error) {
	elig.reset()
	r := wasm.ReaderFrom(body)
	var cached wasm.ModuleInstructionClassifier
	if classifier != nil {
		cached = *classifier
	} else {
		cached = wasm.NewModuleInstructionClassifier(m, true)
	}
	s := byteBodyScanner{r: byteScanReader{Reader: r}, h: h, nLocals: nLocals, nGlobals: nGlobals, selfIdx: selfIdx, elig: elig, entryPrefix: true, memory64: memory64, m: m, classifier: cached, gcTypeLayouts: gcTypeLayouts, gcStructHelpers: gcStructHelpers, moduleHints: moduleHints, importedFuncs: importedFuncs}
	called, term, err := s.scanExpr(0, 0, -1, false)
	if err != nil {
		return s.h, err
	}
	if called {
		s.h.hasCall = true
	}
	if term != 0x0b || s.r.has() {
		return s.h, s.r.err(wasm.ErrInvalidInstruction, s.r.off())
	}
	return s.h, nil
}

type byteBodyScanner struct {
	r        byteScanReader
	h        funcHints
	nLocals  int
	nGlobals int
	selfIdx  uint32
	elig     *globalEligibilityTracker

	entryPrefix     bool
	entrySeen       uint64
	memory64        bool
	m               *wasm.Module
	classifier      wasm.ModuleInstructionClassifier
	gcTypeLayouts   []codegen.GCTypeLayout
	gcStructHelpers bool
	moduleHints     []funcHints
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
	for {
		op, err := s.r.byte()
		if err != nil {
			return true, 0, err
		}
		curIndex := ^uint32(0)
		var curConst int64
		curConstOK := false
		if op == 0x41 || op == 0x42 {
			if b, ok := s.r.Peek(); ok && b&0x80 == 0 {
				curConst = int64(b & 0x7f)
				if b&0x40 != 0 {
					curConst |= ^int64(0x7f)
				}
				curConstOK = true
			}
		}
		if op == 0x43 || op == 0x44 {
			s.h.hasFloatConst = true
		} else if op == 0xfd {
			s.h.hasSIMD = true
		}
		if shared.InstructionNeedsEHFrame(op, wasm.InstrInvalid) {
			s.h.moduleEH = true
		}
		if shared.InstructionNeedsInlineBoundary(op, wasm.InstrInvalid) {
			s.h.hasControlFlow = true
			s.entryPrefix = false
			if op == 0x03 {
				s.h.hasLoop = true
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
				s.h.hasJumpTableData = true
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
						s.h.markGlobalEligible(g)
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
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			s.h.hasCall, subHasCall = true, true
			if op == 0x12 {
				s.h.hasTailCall = true
			}
			if op == 0x10 && imm.Index == s.selfIdx {
				s.h.callsSelf = true
			}
			s.noteDirectCallRef(imm.Index, op == 0x10)
		case 0x11, 0x13, 0x14, 0x15: // indirect/ref calls
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			s.h.hasCall, subHasCall = true, true
			if op == 0x13 || op == 0x15 {
				s.h.hasTailCall = true
			}
		case 0x20, 0x21, 0x22: // local.get/set/tee
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			if op == 0x22 {
				s.h.addStackArenaDiscount(stackLookaheadDiscountOpcode(prevOp))
			}
			idx := imm.Index
			curIndex = idx
			if int(idx) < s.nLocals {
				if s.entryPrefix && idx < 64 {
					bit := uint64(1) << idx
					if s.entrySeen&bit == 0 {
						s.entrySeen |= bit
						if op != 0x20 {
							s.h.entryInitialized |= bit
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
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			idx := imm.Index
			if int(idx) < s.nGlobals {
				if op == 0x24 {
					s.h.addGlobalHotness(idx, 2*loopWeight(loopDepth))
				} else {
					s.h.addGlobalHotness(idx, loopWeight(loopDepth))
				}
				s.elig.add(curLoop, idx)
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0xfc, 0xfd, 0xfe, 0xfb:
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
				s.h.hasControlFlow = true
			}
			if shared.InstructionNeedsEHFrame(op, imm.Kind) {
				s.h.moduleEH = true
			}
			if gcOrAtomicInstructionMayCall(imm.Kind, s.gcStructHelpers) {
				s.h.hasCall, subHasCall = true, true
			}
			if op == 0xfb {
				switch imm.Kind {
				case wasm.InstrStructGet, wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet,
					wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen:
					if directGCResolverInstruction(s.m, s.gcTypeLayouts, imm.Kind, imm.Index, imm.Index2) {
						s.h.gcResolverSites++
					}
				}
			}
			if imm.TouchesMemory {
				s.h.touchesMemory = true
			}
			if imm.UsesBulkMemory {
				s.h.usesBulkMem = true
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
		default:
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
				s.h.hasControlFlow = true
			}
			if shared.InstructionNeedsEHFrame(op, imm.Kind) {
				s.h.moduleEH = true
			}
			if imm.TouchesMemory {
				s.h.touchesMemory = true
			}
			if imm.UsesBulkMemory {
				s.h.usesBulkMem = true
			}
		}
		s.h.addStackArenaDiscount(stackAlgebraicDiscountOpcode(op, prevOp, prevPrevOp, prevIndex, prevPrevIndex, prevConst, prevConstOK))
		prevPrevOp, prevPrevIndex = prevOp, prevIndex
		prevOp, prevIndex = op, curIndex
		prevConst, prevConstOK = curConst, curConstOK
	}
}

func (s *byteBodyScanner) noteDirectCallRef(globalIdx uint32, inline bool) {
	local := int(globalIdx) - s.importedFuncs
	if local < 0 || local >= len(s.moduleHints) {
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
		s.h.mutatesTable = true
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
	if stackArenaOpAllocates(op, imm) {
		s.h.addStackArenaNodes(1)
	}
	if op == 0xfd {
		s.h.hasStackSinkFusion = true
	}
	if stackSinkFusionOpcode(op) {
		next, ok := s.r.Peek()
		if ok && (next == 0x21 || next == 0x22) {
			s.h.hasStackSinkFusion = true
		}
	}
}

func stackSinkFusionCandidate(kind, next wasm.InstrKind) bool {
	if next != wasm.InstrLocalSet && next != wasm.InstrLocalTee {
		return false
	}
	switch kind {
	case wasm.InstrF32Add, wasm.InstrF32Sub, wasm.InstrF32Mul, wasm.InstrF32Div,
		wasm.InstrF64Add, wasm.InstrF64Sub, wasm.InstrF64Mul, wasm.InstrF64Div:
		return true
	default:
		return wasm.IsSIMDValidationInstructionKind(kind)
	}
}

func stackSinkFusionOpcode(op byte) bool {
	return op == 0xfd || op >= 0x92 && op <= 0x95 || op >= 0xa0 && op <= 0xa3
}

func stackLookaheadDiscount(kind wasm.InstrKind) uint16 {
	switch kind {
	case wasm.InstrI64And:
		return 12 // two skipped widen stages, six node-producing instructions each
	case wasm.InstrI64ShrU:
		return 32 // bounded multiply-high tail expansion
	default:
		return 0
	}
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

func stackAlgebraicDiscount(instrs []wasm.Instruction, i int) uint16 {
	kind := instrs[i].Kind
	if i != 0 {
		prev := &instrs[i-1]
		if algebraicIdentityKind(kind, prev.Kind, prev.I32, prev.I64) || kind == wasm.InstrI32WrapI64 && prev.Kind == wasm.InstrI64Or {
			return 1
		}
	}
	if i >= 2 && instrs[i-1].Kind == wasm.InstrLocalGet && instrs[i-2].Kind == wasm.InstrLocalGet &&
		instrs[i-1].Index == instrs[i-2].Index && sameOperandSimplifies(kind) {
		return 1
	}
	return 0
}

func algebraicIdentityKind(kind, constKind wasm.InstrKind, i32 int32, i64 int64) bool {
	var c int64
	switch constKind {
	case wasm.InstrI32Const:
		c = int64(i32)
	case wasm.InstrI64Const:
		c = i64
	default:
		return false
	}
	switch kind {
	case wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32Or, wasm.InstrI32Xor,
		wasm.InstrI32Shl, wasm.InstrI32ShrS, wasm.InstrI32ShrU, wasm.InstrI32Rotl, wasm.InstrI32Rotr,
		wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64Or, wasm.InstrI64Xor,
		wasm.InstrI64Shl, wasm.InstrI64ShrS, wasm.InstrI64ShrU, wasm.InstrI64Rotl, wasm.InstrI64Rotr:
		return c == 0
	case wasm.InstrI32Mul, wasm.InstrI32DivU, wasm.InstrI64Mul, wasm.InstrI64DivU:
		return c == 1
	case wasm.InstrI32And:
		return int32(c) == -1
	case wasm.InstrI64And:
		return c == -1
	default:
		return false
	}
}

func sameOperandSimplifies(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrI32Sub, wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor,
		wasm.InstrI64Sub, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor,
		wasm.InstrI32Eq, wasm.InstrI32Ne, wasm.InstrI32LtS, wasm.InstrI32LtU, wasm.InstrI32GtS, wasm.InstrI32GtU, wasm.InstrI32LeS, wasm.InstrI32LeU, wasm.InstrI32GeS, wasm.InstrI32GeU,
		wasm.InstrI64Eq, wasm.InstrI64Ne, wasm.InstrI64LtS, wasm.InstrI64LtU, wasm.InstrI64GtS, wasm.InstrI64GtU, wasm.InstrI64LeS, wasm.InstrI64LeU, wasm.InstrI64GeS, wasm.InstrI64GeU:
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
	case 0x6a, 0x6b, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		0x7c, 0x7d, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a:
		return c == 0
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
	switch op {
	case 0x6b, 0x71, 0x72, 0x73, 0x7d, 0x83, 0x84, 0x85,
		0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
		0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a:
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
