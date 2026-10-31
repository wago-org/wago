//go:build linux && amd64

package amd64

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Frozen pre-133 recursive scanner preserves the oracle independently of the
// production cold proposal helper split.
func (s *byteBodyScanner) referenceScanExpr133(depth int, loopDepth int, curLoop int, stopAtElse bool) (bool, byte, error) {
	if depth > 20000 {
		return true, 0, s.r.err(wasm.ErrInstructionNestingLimitExceeded, s.r.off())
	}
	subHasCall := false
	weight := loopWeight(loopDepth)
	var prevOp byte
	var prevI32 int32
	var variableShiftDepth uint8
	for {
		op, err := s.r.byte()
		if err != nil {
			return true, 0, err
		}
		s.notePhysicalEvent(op, depth)
		switch op {
		case 0x0b: // end
			return subHasCall, op, nil
		case 0x05: // else
			if stopAtElse {
				return subHasCall, op, nil
			}
			return true, op, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
		case 0x0e: // br_table: exact jump-table-data admission hint
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
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
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
			if op == 0x03 {
				s.h.flags.set(hintHasLoop)
			}
			s.h.noteControlDepth(depth + 1)
			if err := wasm.SkipInstructionImmediate(&s.r.Reader, op); err != nil {
				return true, 0, err
			}
			switch op {
			case 0x02: // block
				calls, term, err := s.referenceScanExpr133(depth+1, loopDepth, curLoop, false)
				if err != nil {
					return true, 0, err
				}
				if term != 0x0b {
					return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
				subHasCall = subHasCall || calls
			case 0x03: // loop
				loop := s.elig.push()
				calls, term, err := s.referenceScanExpr133(depth+1, loopDepth+1, loop, false)
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
				callsThen, term, err := s.referenceScanExpr133(depth+1, loopDepth, curLoop, true)
				if err != nil {
					return true, 0, err
				}
				callsElse := false
				if term == 0x05 {
					callsElse, term, err = s.referenceScanExpr133(depth+1, loopDepth, curLoop, false)
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
			subHasCall = true
			if op == 0x12 {
				s.h.flags.set(hintHasTailCall)
			}
			if op == 0x10 && idx == s.selfIdx {
				s.h.flags.set(hintCallsSelf)
			}
			if tape := s.h.inlineCaller; tape != nil {
				if op == 0x10 {
					tape.record(idx)
				} else {
					tape.blocked = true
				}
			}
			s.noteDirectCallRef(idx, op == 0x10)
		case 0x11, 0x13: // indirect calls
			if tape := s.h.inlineCaller; tape != nil {
				tape.blocked = true
			}
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			s.h.flags.set(hintHasCall)
			s.h.markNonDirectCall()
			if op == 0x14 || op == 0x15 {
				s.h.markUnsupportedDynamicCall()
			}
			subHasCall = true
			if op == 0x13 || op == 0x15 {
				s.h.flags.set(hintHasTailCall)
			}
		case 0x14, 0x15: // call_ref, return_call_ref
			if tape := s.h.inlineCaller; tape != nil {
				tape.blocked = true
			}
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
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
			if int(idx) < s.nLocals {
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
					addHotness(s.h.localScore, idx, weight)
					if int(idx) < len(s.h.localLastGet) {
						s.h.localLastGet[idx] = uint32(s.r.off())
					}
				} else {
					addHotness(s.h.localScore, idx, 2*weight)
				}
			}
		case 0x23, 0x24: // global.get/set
			idx, err := s.r.U32()
			if err != nil {
				return true, 0, err
			}
			if int(idx) < s.nGlobals {
				if op == 0x24 {
					addGlobalHotness(s.globalHints, idx, 2*weight)
				} else {
					addGlobalHotness(s.globalHints, idx, weight)
				}
				s.elig.add(curLoop, idx)
			}
		case 0x41, 0x42: // i32.const, i64.const
			var bits int64
			var typ uint8
			if op == 0x41 {
				v, err := s.r.Reader.I32()
				if err != nil {
					return true, 0, err
				}
				bits, typ = int64(v), 1
				prevI32 = v
			} else {
				v, err := s.r.Reader.I64()
				if err != nil {
					return true, 0, err
				}
				bits, typ = v, 2
			}
			if s.collectLoopIntConsts && loopDepth != 0 {
				if next, ok := s.r.Reader.Peek(); ok && amd64LoopIntConstNeedsRegister(next, bits, typ) {
					s.noteLoopIntConst(bits, loopDepth)
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
		case 0x00, 0x0f: // unreachable, return
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
		case 0x25, 0x26: // table.get/set
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
			if op == 0x26 {
				s.h.flags.set(hintMutatesTable)
			}
		case 0xd2, 0xd5, 0xd6: // ref.func, br_on_null, br_on_non_null
			if _, err := s.r.U32(); err != nil {
				return true, 0, err
			}
			if op != 0xd2 {
				s.h.flags.set(hintHasControlFlow)
				s.entryPrefix = false
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
			// Ordinary scalar accesses cannot call, collect, mutate tables or create
			// GC resolver sites. Consume just their memarg instead of constructing
			// the full proposal classifier payload and rechecking those properties.
			var err error
			if s.m != nil {
				err = s.classifier.SkipScalarMemoryImmediate(&s.r.Reader)
			} else {
				var imm wasm.InstructionImmediate
				err = s.classifyInstructionInto(op, &imm)
			}
			if err != nil {
				return true, 0, err
			}
			s.h.flags.set(hintTouchesMemory)
		case 0x3f, 0x40, 0xfc, 0xfd, 0xfe, 0xfb:
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			if op == 0xfd {
				s.h.flags.set(hintHasSIMD)
			}
			if imm.Kind == wasm.InstrI64MulWideS || imm.Kind == wasm.InstrI64MulWideU {
				s.h.noteFixedScratchLease()
				s.h.noteBoundaryEvent(shared.LocalEventFixedReg, depth)
			}
			if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
				s.h.flags.set(hintHasControlFlow)
			}
			if shared.InstructionNeedsEHFrame(op, imm.Kind) {
				s.h.flags.set(hintModuleEH)
			}
			if gcOrAtomicInstructionMayCall(imm.Kind, s.gcStructHelpers) || shared.SharedMemoryGrowMayCall(s.m, imm.Kind, imm.Index) {
				s.h.noteBoundaryEvent(shared.LocalEventCollection, depth)
				s.h.flags.set(hintHasCall)
				s.h.markUnsupportedDynamicCall()
				subHasCall = true
			}
			if op == 0xfb {
				switch imm.Kind {
				case wasm.InstrArrayFill:
					if prevOp == 0x41 && directGCArrayFillResolver(s.m, s.gcTypeLayouts, imm.Index, prevI32) {
						s.h.addGCResolverSite()
					}
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
			if imm.UsesBulkMemory || imm.Kind == wasm.InstrMemoryInit {
				s.h.noteBoundaryEvent(shared.LocalEventInvalidate, depth)
				s.h.flags.set(hintUsesBulkMem)
			}
		case 0x1f: // try_table: blocktype, catch vector, body
			s.h.flags.set(hintHasControlFlow)
			s.entryPrefix = false
			s.h.flags.set(hintModuleEH)
			s.h.noteControlDepth(depth + 1)
			if err := wasm.SkipInstructionImmediate(&s.r.Reader, op); err != nil {
				return true, 0, err
			}
			calls, term, err := s.referenceScanExpr133(depth+1, loopDepth, curLoop, false)
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
				if count := s.h.immediateFreeOpCount(); count < defaultStackArenaCap {
					s.h.immediateFreeOps = s.h.immediateFreeOps&fixedScratchLeaseMask | count + 1
				}
				break
			}
			var imm wasm.InstructionImmediate
			err := s.classifyInstructionInto(op, &imm)
			if err != nil {
				return true, 0, err
			}
			if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
				s.h.flags.set(hintHasControlFlow)
			}
			if shared.InstructionNeedsEHFrame(op, imm.Kind) {
				s.h.flags.set(hintModuleEH)
			}
			if imm.TouchesMemory {
				s.h.flags.set(hintTouchesMemory)
			}
			if imm.UsesBulkMemory || imm.Kind == wasm.InstrMemoryInit {
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
		prevOp = op
	}
}

type hintScanResult133 struct {
	H                  funcHintView
	Targets            []funcHints
	Selected           [2]int64
	SelectedN          uint8
	EntryPrefix        bool
	Epoch              uint32
	Globals            []shared.GlobalHint
	Marks              []uint32
	Frames             []globalEligibilityFrame
	EligibilityGlobals []uint32
	Calls              bool
	Term               byte
	Offset             int
	Err                error
}

func scanHintCase133(body []byte, memory64, reference bool) hintScanResult133 {
	h := newFuncHints(4, 2)
	var tape shared.LocalEventTape
	tape.Reset(shared.LocalEventLimit)
	h.localEvents = &tape
	var globals shared.GlobalHintAccumulator
	globals.Reset(2)
	elig := newGlobalEligibilityTracker(2)
	targets := make([]funcHints, 4)
	scanner := byteBodyScanner{
		r: byteScanReader{Reader: wasm.ReaderFrom(body)}, h: h, nLocals: 4, nGlobals: 2, selfIdx: 1,
		elig: &elig, globalHints: &globals, entryPrefix: true, memory64: memory64,
		moduleHints: targets, collectLoopIntConsts: true,
	}
	var calls bool
	var term byte
	var err error
	if reference {
		calls, term, err = scanner.referenceScanExpr133(0, 0, -1, false)
	} else {
		calls, term, err = scanner.scanExpr(0, 0, -1, false)
	}
	if scanner.loopIntConstN != 0 {
		scanner.finishLoopIntConsts()
	}
	return hintScanResult133{scanner.h, targets, scanner.selectedLoopIntConsts, scanner.selectedLoopIntConstN, scanner.entryPrefix, elig.epoch, globals.AppendTo(nil), elig.marks, elig.frames, elig.globals, calls, term, scanner.r.off(), err}
}
func compareHintScan133(t *testing.T, body []byte) {
	t.Helper()
	for _, width := range []bool{false, true} {
		got, want := scanHintCase133(body, width, false), scanHintCase133(body, width, true)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("memory64=%v body=%x\ngot=%#v\nwant=%#v", width, body, got, want)
		}
	}
}
func TestHintScanMatchesFrozen133(t *testing.T) {
	cases := [][]byte{
		{0x0b}, {0x05}, {0x1f, 0x40, 0, 0x00, 0x0b, 0x0b},
		{0x02, 0x40, 0x03, 0x40, 0x23, 0, 0x04, 0x40, 0x23, 1, 0x05, 0x10, 0, 0x0b, 0x0b, 0x0b, 0x0b},
		{0x03, 0x40, 0x42, 0x80, 0x80, 0x80, 0x80, 0x10, 0x7c, 0x0b, 0x0b},
		{0x04, 0x40, 0x04, 0x40, 0x05, 0x0b, 0x05, 0x0b, 0x0b},
	}
	for op := 0; op < 256; op++ {
		cases = append(cases, []byte{byte(op), 0, 0, 0, 0, 0, 0x0b, 0x0b, 0x0b})
	}
	rng := rand.New(rand.NewSource(133))
	for range 3000 {
		body := make([]byte, rng.Intn(48))
		rng.Read(body)
		cases = append(cases, body)
	}
	for _, depth := range []int{16, 17, 257, 20001} {
		body := bytes.Repeat([]byte{0x02, 0x40}, depth)
		body = append(body, bytes.Repeat([]byte{0x0b}, depth+1)...)
		cases = append(cases, body)
	}
	for _, body := range cases {
		compareHintScan133(t, body)
	}
}
func FuzzHintScanMatchesFrozen133(f *testing.F) {
	for _, body := range [][]byte{{0x0b}, {0x03, 0x40, 0x23, 1, 0x0b, 0x0b}, {0x04, 0x40, 0x05, 0x0b, 0x0b}, {0xfd, 0, 0, 0, 0x0b}} {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body []byte) { compareHintScan133(t, body) })
}

func TestHintScanNestedMalformedElse133(t *testing.T) {
	compareHintScan133(t, []byte{0x02, 0x40, 0x05})
}
