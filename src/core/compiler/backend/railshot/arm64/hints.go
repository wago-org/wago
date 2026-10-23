//go:build arm64

package arm64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func funcHintStorageBytes(hints []funcHints, sidecar funcHintSidecar) (headers, sidecars uint64) {
	headers = uint64(cap(hints)) * uint64(unsafe.Sizeof(funcHints{}))
	sidecars = uint64(cap(sidecar.localScore)+cap(sidecar.localLastGet)+cap(sidecar.localEventMeta))*uint64(unsafe.Sizeof(uint32(0))) +
		uint64(cap(sidecar.sparseGlobals))*uint64(unsafe.Sizeof(shared.GlobalHint{})) +
		uint64(cap(sidecar.residencyShadow))*uint64(unsafe.Sizeof(shared.ResidencyShadowEntry{})) +
		uint64(cap(sidecar.loopIntConsts))*uint64(unsafe.Sizeof(loopIntConstHintEntry{}))
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

func weightedBranchPath(weight int64) int64 {
	if weight >= maxBranchPathWeight/branchHintWeight {
		return maxBranchPathWeight
	}
	return weight * branchHintWeight
}

type funcHintFlags uint16

const (
	hintHasCall funcHintFlags = 1 << iota
	hintCallsSelf
	hintHasLoop
	hintTouchesMemory
	hintHasInlineLoopCall
	hintUsesBulkMem
	hintMutatesTable
	hintHasControlFlow
	hintModuleEH
	hintHasFloatConst
	hintIntervalRegionStorage
	hintPreservesCallerPins
	hintHasLoopCall
	hintHasNonDirectCall
	hintCallsImport
	hintModuleSIMD
)

// funcHints is everything scanFuncBody yields.
type funcHints struct {
	// memOps packs a saturated memory-op count in the low 20 bits, exact
	// memory-zero offset+width=four accesses in the next 11 bits, and whether a
	// loop contains a memory operation in the high bit. Keeping the
	// secondary frequency in existing header storage preserves the 28-byte hint.
	memOps          uint32
	localStart      uint32
	globalStart     uint32
	globalCount     uint32
	localCount      uint16 // complete parameter-plus-declared-local population
	inlineCallSites uint16 // saturated ordinary direct call sites targeting this local function
	flags           funcHintFlags
	directCallRefs  uint8 // saturated call + return_call references targeting this local function
	// maxControlDepth stores the greatest simultaneously open structured-control
	// depth in its low seven bits. 127 is the saturated fallback sentinel; the
	// high bit records a hot scalar-result join without growing this fixed header.
	maxControlDepth uint8
	// callRelocSites packs a saturated direct-call count in the low 14 bits.
	// The high bits retain sparse loop-constant presence and fail closed for
	// non-table dynamic/helper calls without growing the compact hint record.
	callRelocSites   uint16
	immediateFreeOps uint16 // saturated instruction density
}

const (
	callRelocSiteCountMask          = uint16(1<<14 - 1)
	callRelocLoopIntConstMask       = uint16(1 << 14)
	callRelocUnsupportedDynamicMask = uint16(1 << 15)
)

func (h *funcHints) markUnsupportedDynamicCall() {
	h.callRelocSites |= callRelocUnsupportedDynamicMask
}

// funcHintView reconstructs scan/compile slices on the stack. Only funcHints is
// retained per function; all variable-length data lives in one module sidecar.
type funcHintView struct {
	funcHints
	scalarMergeWeight uint32 // scan-only; retained as one threshold bit in maxControlDepth
	entryInitialized  uint64 // scan-local view; compilation decodes bits during pin planning
	paramAddressSeen  uint64 // scan-local first direct scalar-load use; second use is retained in localScore
	nLocals           int
	localScore        []uint32
	localLastGet      []uint32
	sparseGlobals     []shared.GlobalHint
	localEvents       *shared.LocalEventTape // scan-only, never copied into funcHints
	countLocalEvents  bool                   // ordinary scan retains only the exact count
	localEventCountAt int
	localEventFull    bool
	localEventMeta    uint32 // reconstructed from the sparse sidecar
	residencyShadow   shared.ResidencyShadowSummary
	loopIntConst      [4]int64
	loopIntConstTypes uint8 // two bits per entry: 1=i32, 2=i64, 3=memory page base
	loopIntConstCount uint8
}

type loopIntConstHintEntry struct {
	function uint32
	bits     [4]int64
	types    uint8
	count    uint8
}

type funcHintSidecar struct {
	localScore             []uint32
	localLastGet           []uint32
	sparseGlobals          []shared.GlobalHint
	localEventMeta         []uint32 // ordered localStart, packed event summary pairs
	residencyShadow        []shared.ResidencyShadowEntry
	loopIntConsts          []loopIntConstHintEntry
	localLastGetRangeCount uint32
}

func (s funcHintSidecar) view(h funcHints) funcHintView {
	return s.viewAt(h, -1)
}

// immutableTableHint is one module-owned proof shared by every function
// compilation. It must not be copied into the retained per-function summaries.
type immutableTableHint struct {
	local             bool
	typeKey           uint64
	typed             bool
	monomorphicTarget int
}

const (
	hotScalarMergeBit       = uint8(0x80)
	hotScalarMergeThreshold = uint32(100)
)

func scalarMergeBlockType(bt wasm.BlockType) bool {
	return bt.Kind == wasm.BlockVal && (bt.Val == wasm.I32 || bt.Val == wasm.I64 || bt.Val == wasm.F32 || bt.Val == wasm.F64)
}

const (
	localEventCountMask = uint32(1<<17 - 1)
	localEventOverflow  = uint32(1 << 17)
)

func (h funcHintView) localEventOverflowed() bool {
	return h.localEventMeta&localEventOverflow != 0
}

func newFuncHints(nLocals, nGlobals int) funcHintView {
	h := funcHintsWithStorage(make([]uint32, nLocals))
	h.localLastGet = make([]uint32, nLocals)
	h.localCount = uint16(nLocals)
	h.nLocals = nLocals
	return h
}

const (
	localScoreEntryInitialized = uint32(1 << 31)
	// A self-load-defined i32 local is a dependent-address carrier. Keeping an
	// explicit W-register rename before using it as an address shortens the load
	// dependency chain on Apple ARM cores, so retain this bounded provenance from
	// the existing body scan without allocating another sidecar.
	localScoreLoadDefined = uint32(1 << 30)
	// A parameter used directly as a scalar-load address at least twice can pay
	// one entry canonicalization instead of one canonicalization per load. The
	// first occurrence stays scan-local so the retained score spends one bit only
	// on proven reuse.
	localScoreParamAddressReuse = uint32(1 << 29)
	localScoreHotnessMask       = localScoreParamAddressReuse - 1
)

func finishGlobalHints(h funcHintView, accum *shared.GlobalHintAccumulator) funcHintView {
	h.sparseGlobals = accum.AppendTo(h.sparseGlobals[:0])
	h.globalCount = uint32(len(h.sparseGlobals))
	return h
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

// scanFuncBody chooses the byte-backed scanner used for decoded modules, falling
// back to the AST scanner for tests or callers that construct Func.Body directly.
func scanFuncBody(fn wasm.Func, nLocals, nGlobals int, selfIdx uint32, branchHints []wasm.BranchHint, m *wasm.Module) (funcHintView, error) {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	var accum shared.GlobalHintAccumulator
	accum.Reset(nGlobals)
	h, err := scanFuncBodyIntoModule(fn, nLocals, nGlobals, selfIdx, branchHints, h, &elig, m, nil, nil, 0, &accum, true)
	return finishGlobalHints(h, &accum), err
}

// scanBody performs the AST pre-scan walk. selfIdx is the function's global
// function index (for callsSelf).
func scanBody(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32) funcHintView {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	var accum shared.GlobalHintAccumulator
	accum.Reset(nGlobals)
	return finishGlobalHints(scanBodyInto(body, nLocals, nGlobals, selfIdx, h, &elig, nil, &accum), &accum)
}

func noteASTPhysicalEvent(h *funcHintView, kind wasm.InstrKind, depth int) {
	var event shared.LocalEventKind
	switch kind {
	case wasm.InstrBlock, wasm.InstrTryTable:
		event = shared.LocalEventBlock
	case wasm.InstrLoop:
		event = shared.LocalEventLoop
	case wasm.InstrIf:
		event = shared.LocalEventIf
	case wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn:
		event = shared.LocalEventBranch
	case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrReturnCall,
		wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef:
		event = shared.LocalEventCall
	case wasm.InstrGlobalSet, wasm.InstrTableSet, wasm.InstrMemoryGrow,
		wasm.InstrMemoryInit, wasm.InstrMemoryCopy, wasm.InstrMemoryFill,
		wasm.InstrTableInit, wasm.InstrTableCopy, wasm.InstrTableGrow, wasm.InstrTableFill:
		event = shared.LocalEventInvalidate
	default:
		if gcOrAtomicInstructionMayCall(kind) {
			event = shared.LocalEventCollection
		} else if wasm.IsSIMDValidationInstructionKind(kind) {
			event = shared.LocalEventPressure
		} else {
			return
		}
	}
	h.noteBoundaryEvent(event, depth)
}

func tableSetMayCall(m *wasm.Module, index uint32) bool {
	if m == nil {
		return true
	}
	tt, ok := m.TableType(index)
	if !ok {
		return true
	}
	// Only function and external references are certainly free of the GC write
	// barrier. Indexed reference types are conservatively treated as collector
	// references, without allocating a type lookup table during hint collection.
	if tt.Ref.Heap().Kind() == wasm.HeapAbs {
		switch tt.Ref.Heap().Abs() {
		case wasm.HeapFunc, wasm.HeapNoFunc, wasm.HeapExtern, wasm.HeapNoExtern:
			return false
		}
	}
	return true
}

func scanBodyInto(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32, h funcHintView, elig *globalEligibilityTracker, m *wasm.Module, globalHints *shared.GlobalHintAccumulator) funcHintView {
	elig.reset()
	// walk returns whether the subtree contains a call. curLoop identifies the
	// innermost enclosing loop whose globals are being considered for eligibility.
	var walk func(instrs []wasm.Instruction, depth int, curLoop int) bool
	walk = func(instrs []wasm.Instruction, depth int, curLoop int) bool {
		w := loopWeight(depth)
		sub := false
		for i := range instrs {
			in := &instrs[i]

			if isExactBounds4Kind(in.Kind) {
				memarg := in.MemArg()
				if memarg.Offset == 0 && (memarg.Mem == nil || *memarg.Mem == 0) {
					h.addBounds4Op()
				}
			}
			if depth != 0 && (in.Kind == wasm.InstrBlock || in.Kind == wasm.InstrIf) && scalarMergeBlockType(in.BlockType()) {
				h.addScalarMergeWeight(loopWeight(depth))
			}
			noteASTPhysicalEvent(&h, in.Kind, depth)
			if in.Kind == wasm.InstrF32Const || in.Kind == wasm.InstrF64Const {
				h.flags.set(hintHasFloatConst)
			}
			if wasm.IsSIMDValidationInstructionKind(in.Kind) {
				h.flags.set(hintModuleSIMD)
			}
			if gcOrAtomicInstructionMayCall(in.Kind) || shared.SharedMemoryGrowMayCall(m, in.Kind, in.Index) {
				sub = true
				h.flags.set(hintHasCall)
				h.flags.set(hintHasNonDirectCall)
				h.markUnsupportedDynamicCall()
				if curLoop >= 0 {
					h.flags.set(hintHasLoopCall)
				}
			}
			if shared.InstructionNeedsInlineBoundary(0, in.Kind) {
				h.flags.set(hintHasControlFlow)
				if in.Kind == wasm.InstrLoop {
					h.flags.set(hintHasLoop)
				}
			}
			if shared.InstructionNeedsEHFrame(0, in.Kind) {
				h.flags.set(hintModuleEH)
			}
			if usesBulkScratch(in.Kind) {
				h.flags.set(hintUsesBulkMem)
			}
			switch in.Kind {
			case wasm.InstrCall, wasm.InstrReturnCall, wasm.InstrCallRef, wasm.InstrReturnCallRef:
				sub = true
				h.flags.set(hintHasCall)
				// The legacy AST view does not retain imported-function cardinality.
				// Fail closed for cold-local-call pinning; decoded byte bodies carry
				// the exact local/import distinction below.
				h.flags.set(hintHasNonDirectCall)
				h.markUnsupportedDynamicCall()
				if curLoop >= 0 {
					h.flags.set(hintHasLoopCall)
				}
				if in.Kind == wasm.InstrCall && in.Index == selfIdx {
					h.flags.set(hintCallsSelf)
				}
			case wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect:
				sub = true
				h.flags.set(hintHasCall)
				h.flags.set(hintHasNonDirectCall)
				// Programmatic AST bodies are deliberately fail-closed: the byte
				// scanner is the production path that proves table-only dispatch.
				h.markUnsupportedDynamicCall()
				if curLoop >= 0 {
					h.flags.set(hintHasLoopCall)
				}
			case wasm.InstrLocalGet:
				if int(in.Index) < nLocals {
					h.noteLocalEvent(shared.LocalEventRead, in.Index, depth)
					addHotness(h.localScore, in.Index, w)
				}
			case wasm.InstrLocalSet, wasm.InstrLocalTee:
				if int(in.Index) < nLocals {
					h.noteLocalEvent(shared.LocalEventDefine, in.Index, depth)
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
				h.noteBoundaryEvent(shared.LocalEventEnd, depth)
			case wasm.InstrBlock, wasm.InstrTryTable:
				if walk(in.Body().Instrs, depth, curLoop) {
					sub = true
				}
				h.noteBoundaryEvent(shared.LocalEventEnd, depth)
			case wasm.InstrIf:
				if walk(in.Then(), depth, curLoop) {
					sub = true
				}
				h.noteBoundaryEvent(shared.LocalEventElse, depth)
				if walk(in.Else(), depth, curLoop) {
					sub = true
				}
				h.noteBoundaryEvent(shared.LocalEventEnd, depth)
			case wasm.InstrTableSet, wasm.InstrTableInit, wasm.InstrTableCopy,
				wasm.InstrTableGrow, wasm.InstrTableFill:
				h.flags.set(hintMutatesTable)
			default:
				if instrTouchesMemory(in.Kind) {
					h.flags.set(hintTouchesMemory)
					h.addMemOp()

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
	h, err := scanBodyBytesIntoModule(body, localDeclBytes, nLocals, nGlobals, selfIdx, branchHints, h, &elig, nil, nil, nil, nil, 0, &accum, true)
	return finishGlobalHints(h, &accum), err
}

type loopIntConstCandidate struct {
	bits      int64
	scoreType uint64 // saturated score in low 62 bits; value type in high 2 bits
}

const maxLoopIntConstCandidates = 8

const loopIntConstScoreMask = uint64(1<<62 - 1)

type byteBodyScanner struct {
	r                    byteScanReader
	h                    funcHintView
	nLocals              int
	nGlobals             int
	selfIdx              uint32
	localDeclBytes       uint32
	branchHints          []wasm.BranchHint
	elig                 *globalEligibilityTracker
	globalHints          *shared.GlobalHintAccumulator
	m                    *wasm.Module
	classifier           wasm.ModuleInstructionClassifier
	moduleHints          []funcHints
	parallelCalls        []parallelCalleeHints
	importedFuncs        int
	entryPrefix          bool
	entrySeen            uint64
	collectLoopIntConsts bool
	loopIntConsts        [maxLoopIntConstCandidates]loopIntConstCandidate
	loopIntConstN        uint8
}

type byteScanReader struct{ wasm.Reader }

func (r *byteScanReader) err(code wasm.DecodeErrorCode, off int) error {
	return &wasm.DecodeError{Code: code, Offset: off}
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

const (
	memOpCountBits = 20
	memOpCountMask = uint32(1<<memOpCountBits - 1)

	bounds4OpMask = uint32(1<<(31-memOpCountBits) - 1)
)

func isExactBounds4Kind(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrI32Load, wasm.InstrF32Load, wasm.InstrI64Load32S, wasm.InstrI64Load32U,
		wasm.InstrI32Store, wasm.InstrF32Store, wasm.InstrI64Store32:
		return true
	default:
		return false
	}
}
