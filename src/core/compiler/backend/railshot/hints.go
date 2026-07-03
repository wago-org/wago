package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

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
	hasCall       bool // any direct or indirect call
	callsSelf     bool // a direct call to the function's own index
	touchesMemory bool // any linear-memory op
	usesBulkMem   bool // memory.copy/fill (rep movs/stos clobber RDI/RSI/RCX)

	// Loop-weighted hotness: local.get/global.get = 1×, set/tee = 2×, ×loopWeight
	// per enclosing loop level.
	localScore  []int64
	globalScore []int64

	// globalElig[g]: global g is accessed inside a loop whose subtree contains NO
	// call. Value-pinning such a global in a call-making function is a win: the
	// per-iteration memory traffic disappears while the coherence spill/reload
	// lands only on the (sparse) calls outside that loop. The innermost enclosing
	// loop decides — if it calls, no outer loop can be call-free.
	globalElig []bool
}

func newFuncHints(nLocals, nGlobals int) funcHints {
	return funcHints{
		localScore:  make([]int64, nLocals),
		globalScore: make([]int64, nGlobals),
		globalElig:  make([]bool, nGlobals),
	}
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
	if len(fn.BodyBytes) != 0 {
		return scanBodyBytes(fn.BodyBytes, nLocals, nGlobals, selfIdx)
	}
	return scanBody(fn.Body, nLocals, nGlobals, selfIdx), nil
}

// scanBody performs the AST pre-scan walk. selfIdx is the function's global
// function index (for callsSelf).
func scanBody(body wasm.Expr, nLocals, nGlobals int, selfIdx uint32) funcHints {
	h := newFuncHints(nLocals, nGlobals)
	elig := newGlobalEligibilityTracker(nGlobals)
	// walk returns whether the subtree contains a call. curLoop identifies the
	// innermost enclosing loop whose globals are being considered for eligibility.
	var walk func(instrs []wasm.Instruction, depth int, curLoop int) bool
	walk = func(instrs []wasm.Instruction, depth int, curLoop int) bool {
		w := loopWeight(depth)
		sub := false
		for i := range instrs {
			in := &instrs[i]
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
					h.localScore[in.Index] += w
				}
			case wasm.InstrLocalSet, wasm.InstrLocalTee:
				if int(in.Index) < nLocals {
					h.localScore[in.Index] += 2 * w
				}
			case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
				if int(in.Index) < nGlobals {
					if in.Kind == wasm.InstrGlobalSet {
						h.globalScore[in.Index] += 2 * w
					} else {
						h.globalScore[in.Index] += w
					}
					elig.add(curLoop, in.Index)
				}
			case wasm.InstrLoop:
				loop := elig.push()
				if walk(in.Body().Instrs, depth+1, loop) {
					sub = true // call inside: its globals are not eligible
				} else {
					for _, g := range elig.globalsIn(loop) {
						h.globalElig[g] = true
					}
				}
				elig.pop(loop)
			case wasm.InstrBlock:
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

func scanFuncGlobalScores(fn wasm.Func, nGlobals int, add func(g uint32, score int64)) error {
	if len(fn.BodyBytes) != 0 {
		return scanBodyBytesGlobalScores(fn.BodyBytes, nGlobals, add)
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

func scanBodyBytesGlobalScores(body []byte, nGlobals int, add func(g uint32, score int64)) error {
	s := globalScoreByteScanner{r: byteScanReader{Reader: wasm.NewReader(body)}, nGlobals: nGlobals, add: add}
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
	r        byteScanReader
	nGlobals int
	add      func(g uint32, score int64)
}

func (s *globalScoreByteScanner) scanExpr(depth int, loopDepth int, stopAtElse bool) (byte, error) {
	if depth > 20000 {
		return 0, s.r.err(wasm.ErrInstructionNestingLimitExceeded, s.r.off())
	}
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
			if _, err := s.classifyInstruction(op); err != nil {
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
			imm, err := s.classifyInstruction(op)
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
			if _, err := s.classifyInstruction(op); err != nil {
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
			if _, err := s.classifyInstruction(op); err != nil {
				return 0, err
			}
		}
	}
}

func (s *globalScoreByteScanner) classifyInstruction(op byte) (wasm.InstructionImmediate, error) {
	return wasm.ClassifyInstructionImmediate(s.r.Reader, op)
}

// scanBodyBytes performs the same pre-scan over raw expression bytecode without
// allocating Instruction trees. body includes the terminating end opcode and
// excludes local declarations.
func scanBodyBytes(body []byte, nLocals int, nGlobals int, selfIdx uint32) (funcHints, error) {
	s := byteBodyScanner{r: byteScanReader{Reader: wasm.NewReader(body)}, h: newFuncHints(nLocals, nGlobals), nLocals: nLocals, nGlobals: nGlobals, selfIdx: selfIdx, elig: newGlobalEligibilityTracker(nGlobals)}
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
	elig     globalEligibilityTracker
}

func (s *byteBodyScanner) scanExpr(depth int, loopDepth int, curLoop int, stopAtElse bool) (bool, byte, error) {
	if depth > 20000 {
		return true, 0, s.r.err(wasm.ErrInstructionNestingLimitExceeded, s.r.off())
	}
	subHasCall := false
	for {
		op, err := s.r.byte()
		if err != nil {
			return true, 0, err
		}
		switch op {
		case 0x0b: // end
			return subHasCall, op, nil
		case 0x05: // else
			if stopAtElse {
				return subHasCall, op, nil
			}
			return true, op, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
		case 0x02, 0x03, 0x04: // block, loop, if
			if _, err := s.classifyInstruction(op); err != nil {
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
						s.h.globalElig[g] = true
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
			imm, err := s.classifyInstruction(op)
			if err != nil {
				return true, 0, err
			}
			s.h.hasCall, subHasCall = true, true
			if op == 0x10 && imm.Index == s.selfIdx {
				s.h.callsSelf = true
			}
		case 0x11, 0x13, 0x14, 0x15: // indirect/ref calls
			if _, err := s.classifyInstruction(op); err != nil {
				return true, 0, err
			}
			s.h.hasCall, subHasCall = true, true
		case 0x20, 0x21, 0x22: // local.get/set/tee
			imm, err := s.classifyInstruction(op)
			if err != nil {
				return true, 0, err
			}
			idx := imm.Index
			if int(idx) < s.nLocals {
				if op == 0x20 {
					s.h.localScore[idx] += loopWeight(loopDepth)
				} else {
					s.h.localScore[idx] += 2 * loopWeight(loopDepth)
				}
			}
		case 0x23, 0x24: // global.get/set
			imm, err := s.classifyInstruction(op)
			if err != nil {
				return true, 0, err
			}
			idx := imm.Index
			if int(idx) < s.nGlobals {
				if op == 0x24 {
					s.h.globalScore[idx] += 2 * loopWeight(loopDepth)
				} else {
					s.h.globalScore[idx] += loopWeight(loopDepth)
				}
				s.elig.add(curLoop, idx)
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0xfc, 0xfd, 0xfe, 0xfb:
			imm, err := s.classifyInstruction(op)
			if err != nil {
				return true, 0, err
			}
			if imm.TouchesMemory {
				s.h.touchesMemory = true
			}
			if imm.UsesBulkMemory {
				s.h.usesBulkMem = true
			}
		case 0x1f: // try_table: blocktype, catch vector, body
			if _, err := s.classifyInstruction(op); err != nil {
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
			imm, err := s.classifyInstruction(op)
			if err != nil {
				return true, 0, err
			}
			if imm.TouchesMemory {
				s.h.touchesMemory = true
			}
			if imm.UsesBulkMemory {
				s.h.usesBulkMem = true
			}
		}
	}
}

func (s *byteBodyScanner) classifyInstruction(op byte) (wasm.InstructionImmediate, error) {
	return wasm.ClassifyInstructionImmediate(s.r.Reader, op)
}

type byteScanReader struct{ *wasm.Reader }

func (r *byteScanReader) has() bool { return r.HasNext() }
func (r *byteScanReader) off() int  { return r.Offset() }
func (r *byteScanReader) err(code wasm.DecodeErrorCode, off int) error {
	return &wasm.DecodeError{Code: code, Offset: off}
}
func (r *byteScanReader) byte() (byte, error) { return r.Byte() }

func shouldSkipStackFence(hasCall bool, nLocals int, bodyBytesLen int) bool {
	return !hasCall && frameHdrBytes+8*nLocals+8*bodyBytesLen <= 4096
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
