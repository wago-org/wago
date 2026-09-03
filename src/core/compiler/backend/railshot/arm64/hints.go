//go:build arm64

package arm64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
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
	loopWeightFactor    = 10
	maxLoopWeightDepth  = 6
	branchHintWeight    = 8
	maxBranchPathWeight = int64(1 << 20)
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

func weightedBranchPath(weight int64) int64 {
	if weight >= maxBranchPathWeight/branchHintWeight {
		return maxBranchPathWeight
	}
	return weight * branchHintWeight
}

// funcHints is everything scanFuncBody yields.
type funcHints struct {
	nLocals            int
	hasCall            bool   // any direct or indirect call
	callsSelf          bool   // a direct call to the function's own index
	hasLoop            bool   // structured loop (X12/X13 may be borrowed by loop promotion)
	touchesMemory      bool   // any linear-memory op
	inlineCallSites    uint16 // saturated ordinary direct call sites targeting this local function
	directCallRefs     uint8  // saturated call + return_call references targeting this local function
	hasInlineLoopCall  bool   // an ordinary direct call site is nested in a loop
	memOps             int    // scalar/vector/bulk linear-memory instructions
	usesBulkMem        bool   // memory.copy/fill (explicit LDRB/STRB copy/fill loop clobbers X16/X17 + call scratch)
	mutatesTable       bool   // table.set/init/copy/grow/fill; excludes immutable local-table call_indirect specialization
	hasControlFlow     bool   // control opcode relevant to inline splice framing
	moduleEH           bool   // module-wide: reserve the active exception-handler register
	hasStackSinkFusion bool   // lowering may allocate fewer nodes than the scan; retain the legacy arena

	// entryInitialized marks locals whose first access in the straight-line entry
	// prefix is local.set/tee, making their declared zero unobservable.
	entryInitialized uint64

	localStart  uint32
	globalStart uint32
	globalCount uint32

	// stackArenaNodes is a conservative pre-scan estimate of operand-stack elem
	// allocations while compiling this body. It lets compileFunc avoid reserving
	// arena nodes for long immediates (notably v128.const payload bytes) while the
	// stack's heap fallback still preserves pointer stability if the estimate is
	// low for unusual control flow.
	stackArenaNodes    uint32
	stackArenaDiscount uint16 // possible scanned nodes removed by bounded lookahead peepholes
}

// funcHintView reconstructs scan/compile slices on the stack. Only funcHints is
// retained per function; all variable-length data lives in one module sidecar.
type funcHintView struct {
	funcHints
	localScore    []uint32
	localLastGet  []uint32
	sparseGlobals []shared.GlobalHint
}

type funcHintSidecar struct {
	localScore    []uint32
	localLastGet  []uint32
	sparseGlobals []shared.GlobalHint
}

func (s funcHintSidecar) view(h funcHints) funcHintView {
	localStart := int(h.localStart)
	localEnd := localStart + h.nLocals
	globalStart := int(h.globalStart)
	globalEnd := globalStart + int(h.globalCount)
	return funcHintView{
		funcHints:     h,
		localScore:    s.localScore[localStart:localEnd],
		localLastGet:  s.localLastGet[localStart:localEnd],
		sparseGlobals: s.sparseGlobals[globalStart:globalEnd],
	}
}

// immutableTableHint is one module-owned proof shared by every function
// compilation. It must not be copied into the retained per-function summaries.
type immutableTableHint struct {
	local             bool
	typeKey           uint64
	typed             bool
	monomorphicTarget int
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
	if ^uint16(0)-h.stackArenaDiscount < n {
		h.stackArenaDiscount = ^uint16(0)
	} else {
		h.stackArenaDiscount += n
	}
}

func newFuncHints(nLocals, nGlobals int) funcHintView {
	h := funcHintsWithStorage(make([]uint32, nLocals))
	h.localLastGet = make([]uint32, nLocals)
	h.nLocals = nLocals
	return h
}

func funcHintsWithStorage(localScore []uint32) funcHintView {
	return funcHintView{localScore: localScore}
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
	const max = ^uint32(0)
	if uint64(scores[idx])+uint64(delta) >= uint64(max) {
		scores[idx] = max
	} else {
		scores[idx] += uint32(delta)
	}
}

func addGlobalHotness(accum *shared.GlobalHintAccumulator, idx uint32, delta int64) {
	accum.Add(idx, delta)
}

func markGlobalEligible(accum *shared.GlobalHintAccumulator, idx uint32) {
	accum.MarkEligible(idx)
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
func scanFuncBody(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, branchHints []wasm.BranchHint, m *wasm.Module) (funcHintView, error) {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	var accum shared.GlobalHintAccumulator
	accum.Reset(nGlobals)
	h, err := scanFuncBodyIntoModule(fn, nLocals, nGlobals, selfIdx, branchHints, h, &elig, m, nil, nil, 0, &accum)
	return finishGlobalHints(h, &accum), err
}

func scanFuncBodyIntoModule(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, branchHints []wasm.BranchHint, h funcHintView, elig *globalEligibilityTracker, m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, moduleHints []funcHints, importedFuncs int, globalHints *shared.GlobalHintAccumulator) (funcHintView, error) {
	if len(fn.BodyBytes) != 0 {
		return scanBodyBytesIntoModule(fn.BodyBytes, fn.LocalDeclBytes, nLocals, nGlobals, selfIdx, branchHints, h, elig, m, classifier, moduleHints, importedFuncs, globalHints)
	}
	return scanBodyInto(fn.Body, nLocals, nGlobals, selfIdx, h, elig, globalHints), nil
}

// scanBody performs the AST pre-scan walk. selfIdx is the function's global
// function index (for callsSelf).
func scanBody(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32) funcHintView {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	var accum shared.GlobalHintAccumulator
	accum.Reset(nGlobals)
	return finishGlobalHints(scanBodyInto(body, nLocals, nGlobals, selfIdx, h, &elig, &accum), &accum)
}

func gcOrAtomicInstructionMayCall(kind wasm.InstrKind) bool {
	switch kind {
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

func scanBodyInto(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32, h funcHintView, elig *globalEligibilityTracker, globalHints *shared.GlobalHintAccumulator) funcHintView {
	elig.reset()
	// Programmatic decoded bodies are uncommon and can exercise context-sensitive
	// folds that the production byte scan accounts for. Keep their legacy arena.
	if shared.StackArenaHintsEnabled {
		h.hasStackSinkFusion = true
	}
	// walk returns whether the subtree contains a call. curLoop identifies the
	// innermost enclosing loop whose globals are being considered for eligibility.
	var walk func(instrs []wasm.Instruction, depth int, curLoop int) bool
	walk = func(instrs []wasm.Instruction, depth int, curLoop int) bool {
		w := loopWeight(depth)
		sub := false
		for i := range instrs {
			in := &instrs[i]
			if gcOrAtomicInstructionMayCall(in.Kind) {
				sub, h.hasCall = true, true
			}
			if shared.InstructionNeedsInlineBoundary(0, in.Kind) {
				h.hasControlFlow = true
				if in.Kind == wasm.InstrLoop {
					h.hasLoop = true
				}
			}
			if shared.InstructionNeedsEHFrame(0, in.Kind) {
				h.moduleEH = true
			}
			switch in.Kind {
			case wasm.InstrCall, wasm.InstrReturnCall, wasm.InstrCallRef, wasm.InstrReturnCallRef:
				sub, h.hasCall = true, true
				if in.Kind == wasm.InstrCall && in.Index == selfIdx {
					h.callsSelf = true
				}
			case wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect:
				sub, h.hasCall = true, true
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
				h.usesBulkMem, h.touchesMemory = true, true
				h.memOps++
			case wasm.InstrTableSet, wasm.InstrTableInit, wasm.InstrTableCopy,
				wasm.InstrTableGrow, wasm.InstrTableFill:
				h.mutatesTable = true
			default:
				if instrTouchesMemory(in.Kind) {
					h.touchesMemory = true
					h.memOps++
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
	return scanBodyBytesWithHints(body, 0, nLocals, nGlobals, selfIdx, nil)
}

func scanBodyBytesWithHints(body []byte, localDeclBytes uint32, nLocals int, nGlobals int, selfIdx uint32, branchHints []wasm.BranchHint) (funcHintView, error) {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	var accum shared.GlobalHintAccumulator
	accum.Reset(nGlobals)
	h, err := scanBodyBytesIntoModule(body, localDeclBytes, nLocals, nGlobals, selfIdx, branchHints, h, &elig, nil, nil, nil, 0, &accum)
	return finishGlobalHints(h, &accum), err
}

func scanBodyBytesIntoModule(body []byte, localDeclBytes uint32, nLocals int, nGlobals int, selfIdx uint32, branchHints []wasm.BranchHint, h funcHintView, elig *globalEligibilityTracker, m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, moduleHints []funcHints, importedFuncs int, globalHints *shared.GlobalHintAccumulator) (funcHintView, error) {
	elig.reset()
	r := wasm.ReaderFrom(body)
	var cached wasm.ModuleInstructionClassifier
	if classifier != nil {
		cached = *classifier
	} else {
		cached = wasm.NewModuleInstructionClassifier(m, true)
	}
	s := byteBodyScanner{r: byteScanReader{Reader: r}, h: h, nLocals: nLocals, nGlobals: nGlobals, selfIdx: selfIdx, localDeclBytes: localDeclBytes, branchHints: branchHints, elig: elig, globalHints: globalHints, m: m, classifier: cached, moduleHints: moduleHints, importedFuncs: importedFuncs, entryPrefix: true}
	called, term, err := s.scanExpr(0, 0, -1, false, 1)
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
	r              byteScanReader
	h              funcHintView
	nLocals        int
	nGlobals       int
	selfIdx        uint32
	localDeclBytes uint32
	branchHints    []wasm.BranchHint
	elig           *globalEligibilityTracker
	globalHints    *shared.GlobalHintAccumulator
	m              *wasm.Module
	classifier     wasm.ModuleInstructionClassifier
	moduleHints    []funcHints
	importedFuncs  int
	entryPrefix    bool
	entrySeen      uint64
}

func (s *byteBodyScanner) scanExpr(depth int, loopDepth int, curLoop int, stopAtElse bool, pathWeight int64) (bool, byte, error) {
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
		if shared.StackArenaHintsEnabled && (op == 0x41 || op == 0x42) {
			if b, ok := s.r.Peek(); ok && b&0x80 == 0 {
				curConst = int64(b & 0x7f)
				if b&0x40 != 0 {
					curConst |= ^int64(0x7f)
				}
				curConstOK = true
			}
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
		case 0x02, 0x03, 0x04: // block, loop, if
			opOffset := s.localDeclBytes + uint32(s.r.off()-1)
			s.h.addStackArenaNodes(2) // entry flush/rebuild allowance.
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
				s.h.hasLoop = true
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
				if next, ok := s.r.Peek(); ok && (next == 0x21 || next == 0x22) {
					s.h.hasStackSinkFusion = true
				}
			}
		case 0x10, 0x12: // call, return_call
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			s.h.hasCall, subHasCall = true, true
			if op == 0x10 && imm.Index == s.selfIdx {
				s.h.callsSelf = true
			}
			s.noteDirectCallRef(imm.Index, op == 0x10, loopDepth != 0)
		case 0x11, 0x13, 0x14, 0x15: // indirect/ref calls
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
			s.h.hasCall, subHasCall = true, true
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
					addHotness(s.h.localScore, idx, pathWeight*loopWeight(loopDepth))
					if int(idx) < len(s.h.localLastGet) {
						s.h.localLastGet[idx] = uint32(s.r.off())
					}
				} else {
					addHotness(s.h.localScore, idx, 2*pathWeight*loopWeight(loopDepth))
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
					addGlobalHotness(s.globalHints, idx, 2*pathWeight*loopWeight(loopDepth))
				} else {
					addGlobalHotness(s.globalHints, idx, pathWeight*loopWeight(loopDepth))
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
			if op == 0xfb {
				// Collector-backed GC instructions may enter the synchronous Go
				// helper bridge. Preserve LR and use call-safe local state for the
				// whole family; direct-only subopcodes pay only the frame-record cost.
				s.h.hasCall, subHasCall = true, true
			}
			switch imm.Kind {
			case wasm.InstrMemoryAtomicNotify, wasm.InstrMemoryAtomicWait32, wasm.InstrMemoryAtomicWait64:
				s.h.hasCall, subHasCall = true, true
			}
			if imm.TouchesMemory {
				s.h.touchesMemory = true
				s.h.memOps++
			}
			if imm.UsesBulkMemory {
				s.h.usesBulkMem = true
			}
		case 0x1f: // try_table: blocktype, catch vector, body
			s.h.moduleEH = true
			s.h.addStackArenaNodes(2) // entry flush/rebuild allowance.
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
		case 0x08, 0x0a: // throw, throw_ref
			s.h.moduleEH = true
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.noteStackArenaOp(op, &imm)
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
				s.h.memOps++
			}
			if imm.UsesBulkMemory {
				s.h.usesBulkMem = true
			}
		}
		if shared.StackArenaHintsEnabled {
			if !prevConstOK && (prevOp == 0x41 || prevOp == 0x42) &&
				(op >= 0x6a && op <= 0x78 || op >= 0x7c && op <= 0x8a) {
				s.h.hasStackSinkFusion = true
			}
			if stackFlowTerminatorOpcode(op) {
				if next, ok := s.r.Peek(); ok && next != 0x0b && next != 0x05 {
					s.h.hasStackSinkFusion = true
				}
			}
			s.h.addStackArenaDiscount(stackAlgebraicDiscountOpcode(op, prevOp, prevPrevOp, prevIndex, prevPrevIndex, prevConst, prevConstOK))
			prevPrevOp, prevPrevIndex = prevOp, prevIndex
			prevOp, prevIndex = op, curIndex
			prevConst, prevConstOK = curConst, curConstOK
		}
	}
}

func (s *byteBodyScanner) noteDirectCallRef(globalIdx uint32, inline, inLoop bool) {
	local := int(globalIdx) - s.importedFuncs
	if local < 0 || local >= len(s.moduleHints) {
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
		target.hasInlineLoopCall = true
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
	if !shared.StackArenaHintsEnabled {
		return
	}
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

func stackSinkFusionOpcode(op byte) bool {
	return op == 0x1b || op == 0x1c || op >= 0x92 && op <= 0x97 || op >= 0xa0 && op <= 0xa5
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
