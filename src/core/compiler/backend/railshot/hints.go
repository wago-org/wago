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
	// walk returns whether the subtree contains a call. cur collects the globals
	// whose innermost enclosing loop is the currently open one (nil outside loops).
	var walk func(instrs []wasm.Instruction, depth int, cur *[]uint32) bool
	walk = func(instrs []wasm.Instruction, depth int, cur *[]uint32) bool {
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
					if cur != nil {
						*cur = append(*cur, in.Index)
					}
				}
			case wasm.InstrLoop:
				var mine []uint32
				if walk(in.Body().Instrs, depth+1, &mine) {
					sub = true // call inside: its globals are not eligible
				} else {
					for _, g := range mine {
						h.globalElig[g] = true
					}
				}
			case wasm.InstrBlock:
				if walk(in.Body().Instrs, depth, cur) {
					sub = true
				}
			case wasm.InstrIf:
				if walk(in.Then(), depth, cur) {
					sub = true
				}
				if walk(in.Else(), depth, cur) {
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
	walk(body.Instrs, 0, nil)
	return h
}

// scanBodyBytes performs the same pre-scan over raw expression bytecode without
// allocating Instruction trees. body includes the terminating end opcode and
// excludes local declarations.
func scanBodyBytes(body []byte, nLocals int, nGlobals int, selfIdx uint32) (funcHints, error) {
	s := byteBodyScanner{r: byteScanReader{data: body}, h: newFuncHints(nLocals, nGlobals), nLocals: nLocals, nGlobals: nGlobals, selfIdx: selfIdx}
	called, term, err := s.scanExpr(0, 0, nil, false)
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
}

func (s *byteBodyScanner) scanExpr(depth int, loopDepth int, cur *[]uint32, stopAtElse bool) (bool, byte, error) {
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
			if err := s.skipBlockType(); err != nil {
				return true, 0, err
			}
			switch op {
			case 0x02: // block
				calls, term, err := s.scanExpr(depth+1, loopDepth, cur, false)
				if err != nil {
					return true, 0, err
				}
				if term != 0x0b {
					return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
				subHasCall = subHasCall || calls
			case 0x03: // loop
				var mine []uint32
				calls, term, err := s.scanExpr(depth+1, loopDepth+1, &mine, false)
				if err != nil {
					return true, 0, err
				}
				if term != 0x0b {
					return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
				if calls {
					subHasCall = true
				} else {
					for _, g := range mine {
						s.h.globalElig[g] = true
					}
				}
			case 0x04: // if
				callsThen, term, err := s.scanExpr(depth+1, loopDepth, cur, true)
				if err != nil {
					return true, 0, err
				}
				callsElse := false
				if term == 0x05 {
					callsElse, term, err = s.scanExpr(depth+1, loopDepth, cur, false)
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
			idx, err := s.r.u32()
			if err != nil {
				return true, 0, err
			}
			s.h.hasCall, subHasCall = true, true
			if op == 0x10 && idx == s.selfIdx {
				s.h.callsSelf = true
			}
		case 0x11, 0x13: // call_indirect, return_call_indirect
			if err := s.r.skipU32N(2); err != nil {
				return true, 0, err
			}
			s.h.hasCall, subHasCall = true, true
		case 0x14, 0x15: // call_ref, return_call_ref
			if err := s.r.skipU32N(1); err != nil {
				return true, 0, err
			}
			s.h.hasCall, subHasCall = true, true
		case 0x20, 0x21, 0x22: // local.get/set/tee
			idx, err := s.r.u32()
			if err != nil {
				return true, 0, err
			}
			if int(idx) < s.nLocals {
				if op == 0x20 {
					s.h.localScore[idx] += loopWeight(loopDepth)
				} else {
					s.h.localScore[idx] += 2 * loopWeight(loopDepth)
				}
			}
		case 0x23, 0x24: // global.get/set
			idx, err := s.r.u32()
			if err != nil {
				return true, 0, err
			}
			if int(idx) < s.nGlobals {
				if op == 0x24 {
					s.h.globalScore[idx] += 2 * loopWeight(loopDepth)
				} else {
					s.h.globalScore[idx] += loopWeight(loopDepth)
				}
				if cur != nil {
					*cur = append(*cur, idx)
				}
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
			s.h.touchesMemory = true
			if err := s.skipMemArg(); err != nil {
				return true, 0, err
			}
		case 0x3f, 0x40: // memory.size/grow
			s.h.touchesMemory = true
			if err := s.r.skipU32N(1); err != nil {
				return true, 0, err
			}
		case 0xfc:
			calls, err := s.scanFC()
			if err != nil {
				return true, 0, err
			}
			subHasCall = subHasCall || calls
		case 0xfd:
			if err := s.scanFD(); err != nil {
				return true, 0, err
			}
		case 0xfe:
			if err := s.scanFE(); err != nil {
				return true, 0, err
			}
		case 0xfb:
			if err := s.scanFB(); err != nil {
				return true, 0, err
			}
		case 0x1f: // try_table: blocktype, catch vector, body
			if err := s.skipBlockType(); err != nil {
				return true, 0, err
			}
			if err := s.skipCatchVec(); err != nil {
				return true, 0, err
			}
			calls, term, err := s.scanExpr(depth+1, loopDepth, cur, false)
			if err != nil {
				return true, 0, err
			}
			if term != 0x0b {
				return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
			}
			subHasCall = subHasCall || calls
		default:
			if err := s.skipPlainInstruction(op); err != nil {
				return true, 0, err
			}
		}
	}
}

func (s *byteBodyScanner) scanFC() (bool, error) {
	sub, err := s.r.u32()
	if err != nil {
		return true, err
	}
	switch sub {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		return false, nil
	case 8: // memory.init
		s.h.touchesMemory = true
		return false, s.r.skipU32N(2)
	case 9, 13, 15, 16, 17:
		return false, s.r.skipU32N(1)
	case 10: // memory.copy
		s.h.touchesMemory, s.h.usesBulkMem = true, true
		return false, s.r.skipU32N(2)
	case 11: // memory.fill
		s.h.touchesMemory, s.h.usesBulkMem = true, true
		return false, s.r.skipU32N(1)
	case 12, 14:
		return false, s.r.skipU32N(2)
	default:
		return true, s.r.err(wasm.ErrInvalidInstruction, s.r.off())
	}
}

func (s *byteBodyScanner) scanFB() error {
	sub, err := s.r.u32()
	if err != nil {
		return err
	}
	switch sub {
	case 15, 26, 27, 28, 29, 30, 0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7:
		return nil
	case 0, 1, 6, 7, 11, 12, 13, 14, 16, 32, 33, 34, 0x82:
		return s.r.skipU32N(1)
	case 2, 3, 4, 5, 8, 9, 10, 17, 18, 19:
		return s.r.skipU32N(2)
	case 20, 21, 35, 36:
		return s.skipHeapTypeWithExact()
	case 22, 23:
		return s.skipRefHeapType()
	case 24, 25:
		if err := s.r.skipBytes(1); err != nil { // cast flags
			return err
		}
		if err := s.r.skipU32N(1); err != nil { // label
			return err
		}
		if err := s.skipHeapTypeWithExact(); err != nil {
			return err
		}
		return s.skipHeapTypeWithExact()
	default:
		return s.r.err(wasm.ErrInvalidInstruction, s.r.off())
	}
}

func (s *byteBodyScanner) scanFD() error {
	sub, err := s.r.u32()
	if err != nil {
		return err
	}
	if sub == 12 || sub == 13 {
		return s.r.skipBytes(16)
	}
	if isFDMem(sub) {
		s.h.touchesMemory = true
		if err := s.skipMemArg(); err != nil {
			return err
		}
		if sub >= 84 && sub <= 91 {
			return s.r.skipBytes(1)
		}
		return nil
	}
	if isFDLane(sub) {
		return s.r.skipBytes(1)
	}
	if isFDNoImm(sub) {
		return nil
	}
	return s.r.err(wasm.ErrInvalidInstruction, s.r.off())
}

func (s *byteBodyScanner) scanFE() error {
	sub, err := s.r.u32()
	if err != nil {
		return err
	}
	if sub == 0x03 {
		b, err := s.r.byte()
		if err != nil {
			return err
		}
		if b != 0 {
			return s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
		}
		return nil
	}
	if sub >= 0x5c && sub <= 0x5e {
		if err := s.r.skipBytes(1); err != nil { // atomic order
			return err
		}
		return s.r.skipU32N(2)
	}
	if isFEMem(sub) || (sub >= 30 && sub <= 78) {
		s.h.touchesMemory = true
		return s.skipMemArg()
	}
	return s.r.err(wasm.ErrInvalidInstruction, s.r.off())
}

func (s *byteBodyScanner) skipPlainInstruction(op byte) error {
	if isSimpleOpcode(op) {
		return nil
	}
	switch op {
	case 0x08, 0x0c, 0x0d, 0x25, 0x26, 0xd2, 0xd5, 0xd6:
		return s.r.skipU32N(1)
	case 0x0e:
		n, err := s.r.u32()
		if err != nil {
			return err
		}
		if err := s.r.skipU32N(n); err != nil {
			return err
		}
		return s.r.skipU32N(1)
	case 0x1c:
		return s.skipResultType()
	case 0x41:
		_, err := s.r.i32()
		return err
	case 0x42:
		_, err := s.r.i64()
		return err
	case 0x43:
		return s.r.skipBytes(4)
	case 0x44:
		return s.r.skipBytes(8)
	case 0xd0:
		return s.skipRefHeapType()
	case 0xd3, 0xd4:
		return nil
	default:
		return s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
	}
}

func (s *byteBodyScanner) skipBlockType() error {
	b, ok := s.r.peek()
	if !ok {
		return s.r.err(wasm.ErrInvalidBlockType, s.r.off())
	}
	if b == 0x40 || isValTypeLead(b) {
		if b == 0x40 {
			_, _ = s.r.byte()
			return nil
		}
		return s.skipValType()
	}
	_, err := s.r.s33()
	return err
}

func (s *byteBodyScanner) skipResultType() error {
	n, err := s.r.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		if err := s.skipValType(); err != nil {
			return err
		}
	}
	return nil
}

func (s *byteBodyScanner) skipValType() error {
	b, err := s.r.byte()
	if err != nil {
		return err
	}
	switch b {
	case 0x7f, 0x7e, 0x7d, 0x7c, 0x7b:
		return nil
	case 0x63, 0x64:
		// A bare 0x64 may be stringref; if more bytes are present and they form a
		// heap type, consume them like decodeValType does for ref types.
		if b == 0x64 {
			p := s.r.pos
			if err := s.skipRefHeapType(); err != nil {
				s.r.pos = p
				return nil
			}
			return nil
		}
		return s.skipRefHeapType()
	case 0x6f, 0x70, 0x6e, 0x6d, 0x6c, 0x6b, 0x6a, 0x69, 0x71, 0x72, 0x73, 0x74:
		return nil
	default:
		return s.r.err(wasm.ErrInvalidType, s.r.off()-1)
	}
}

func (s *byteBodyScanner) skipRefHeapType() error { return s.skipHeapTypeWithExact() }

func (s *byteBodyScanner) skipHeapTypeWithExact() error {
	if b, ok := s.r.peek(); ok && b == 0x62 {
		_, _ = s.r.byte()
		_, err := s.r.s33()
		return err
	}
	return s.skipHeapType()
}

func (s *byteBodyScanner) skipHeapType() error {
	if b, ok := s.r.peek(); ok && (b == 0x64 || (b >= 0x69 && b <= 0x74)) {
		_, _ = s.r.byte()
		return nil
	}
	_, err := s.r.s33()
	return err
}

func (s *byteBodyScanner) skipMemArg() error {
	n, err := s.r.u32()
	if err != nil {
		return err
	}
	if n >= 64 && n < 128 {
		if err := s.r.skipU32N(1); err != nil {
			return err
		}
	} else if n >= 64 {
		return s.r.err(wasm.ErrInvalidInstruction, s.r.off())
	}
	_, err = s.r.u64()
	return err
}

func (s *byteBodyScanner) skipCatchVec() error {
	n, err := s.r.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		kind, err := s.r.byte()
		if err != nil {
			return err
		}
		switch kind {
		case 0, 1:
			if err := s.r.skipU32N(1); err != nil {
				return err
			}
			if err := s.r.skipU32N(1); err != nil {
				return err
			}
		case 2, 3:
			if err := s.r.skipU32N(1); err != nil {
				return err
			}
		default:
			return s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
		}
	}
	return nil
}

type byteScanReader struct {
	data []byte
	pos  int
}

func (r *byteScanReader) has() bool { return r.pos < len(r.data) }
func (r *byteScanReader) off() int  { return r.pos }
func (r *byteScanReader) peek() (byte, bool) {
	if r.pos >= len(r.data) {
		return 0, false
	}
	return r.data[r.pos], true
}
func (r *byteScanReader) err(code wasm.DecodeErrorCode, off int) error {
	return &wasm.DecodeError{Code: code, Offset: off}
}
func (r *byteScanReader) byte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, r.err(wasm.ErrIndexOutOfBounds, r.pos)
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}
func (r *byteScanReader) skipBytes(n int) error {
	if n < 0 || r.pos+n > len(r.data) {
		return r.err(wasm.ErrIndexOutOfBounds, r.pos)
	}
	r.pos += n
	return nil
}
func (r *byteScanReader) skipU32N(n uint32) error {
	for i := uint32(0); i < n; i++ {
		if _, err := r.u32(); err != nil {
			return err
		}
	}
	return nil
}
func (r *byteScanReader) leb(signed bool, maxBits uint32) (uint64, error) {
	var result uint64
	var shift uint32
	for i := 0; ; i++ {
		if i >= int((maxBits+6)/7) {
			return 0, r.err(wasm.ErrMalformedLEB, r.pos)
		}
		b, err := r.byte()
		if err != nil {
			return 0, err
		}
		if shift >= 64 && (b&0x7f) != 0 {
			return 0, r.err(wasm.ErrMalformedLEB, r.pos)
		}
		if shift < 64 {
			result |= uint64(b&0x7f) << shift
		}
		cont := b&0x80 != 0
		shift += 7
		if !cont {
			if shift > maxBits {
				extra := shift - maxBits
				used := 7 - extra
				mask := byte(((uint16(1) << extra) - 1) << used)
				if signed && (b&(1<<(used-1))) != 0 {
					if b&mask != mask {
						return 0, r.err(wasm.ErrMalformedLEB, r.pos)
					}
				} else if b&mask != 0 {
					return 0, r.err(wasm.ErrMalformedLEB, r.pos)
				}
			}
			if signed && shift < 64 && (b&0x40) != 0 {
				result |= ^uint64(0) << shift
			}
			return result, nil
		}
		if shift >= maxBits+7 {
			return 0, r.err(wasm.ErrMalformedLEB, r.pos)
		}
	}
}
func (r *byteScanReader) u32() (uint32, error) { v, err := r.leb(false, 32); return uint32(v), err }
func (r *byteScanReader) u64() (uint64, error) { return r.leb(false, 64) }
func (r *byteScanReader) s33() (int64, error)  { v, err := r.leb(true, 33); return int64(v), err }
func (r *byteScanReader) i32() (int32, error)  { v, err := r.leb(true, 32); return int32(v), err }
func (r *byteScanReader) i64() (int64, error)  { v, err := r.leb(true, 64); return int64(v), err }

func isValTypeLead(b byte) bool {
	switch b {
	case 0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x63, 0x64, 0x6f, 0x70, 0x6e, 0x6d, 0x6c, 0x6b, 0x6a, 0x69, 0x71, 0x72, 0x73, 0x74:
		return true
	default:
		return false
	}
}

func isSimpleOpcode(op byte) bool {
	switch op {
	case 0x00, 0x01, 0x0a, 0x0f, 0x1a, 0x1b, 0xd1,
		0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
		0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
		0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66,
		0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f, 0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a,
		0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
		0x99, 0x9a, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f, 0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6,
		0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf, 0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf,
		0xc0, 0xc1, 0xc2, 0xc3, 0xc4:
		return true
	default:
		return false
	}
}

func isFDMem(sub uint32) bool { return sub <= 11 || (sub >= 84 && sub <= 93) }
func isFDLane(sub uint32) bool {
	return (sub >= 21 && sub <= 34)
}
func isFDNoImm(sub uint32) bool {
	return (sub >= 14 && sub <= 20) || (sub >= 35 && sub <= 83) || (sub >= 94 && sub <= 135) || (sub >= 137 && sub <= 149) || (sub >= 151 && sub <= 153) || (sub >= 155 && sub <= 160) || (sub >= 163 && sub <= 164) || (sub >= 167 && sub <= 174) || sub == 177 || (sub >= 181 && sub <= 186) || (sub >= 188 && sub <= 192) || (sub >= 195 && sub <= 196) || (sub >= 199 && sub <= 206) || sub == 209 || (sub >= 213 && sub <= 224) || (sub >= 227 && sub <= 236) || sub == 237 || (sub >= 239 && sub <= 275)
}
func isFEMem(sub uint32) bool {
	return sub <= 0x02 || (sub >= 0x10 && sub <= 0x1d)
}

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
