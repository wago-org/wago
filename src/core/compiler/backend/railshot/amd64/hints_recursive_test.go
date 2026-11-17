// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (s *byteBodyScanner) scanExprRecursive(depth int, loopDepth int, curLoop int, stopAtElse bool) (bool, byte, error) {
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
				calls, term, err := s.scanExprRecursive(depth+1, loopDepth, curLoop, false)
				if err != nil {
					return true, 0, err
				}
				if term != 0x0b {
					return true, term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
				subHasCall = subHasCall || calls
			case 0x03: // loop
				loop := s.elig.push()
				calls, term, err := s.scanExprRecursive(depth+1, loopDepth+1, loop, false)
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
				callsThen, term, err := s.scanExprRecursive(depth+1, loopDepth, curLoop, true)
				if err != nil {
					return true, 0, err
				}
				callsElse := false
				if term == 0x05 {
					callsElse, term, err = s.scanExprRecursive(depth+1, loopDepth, curLoop, false)
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
				err = s.classifyUnboundHintMemory(op)
			}
			if err != nil {
				return true, 0, err
			}
			s.h.flags.set(hintTouchesMemory)
		case 0x3f, 0x40, 0xfc, 0xfd, 0xfe, 0xfb:
			calls, err := s.scanExtendedHint(op, depth, prevOp, prevI32)
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
			calls, term, err := s.scanExprRecursive(depth+1, loopDepth, curLoop, false)
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
			if err := s.scanFallbackHint(op); err != nil {
				return true, 0, err
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

// Proposal payloads are cold for ordinary scalar functions. Keep them out of
// every recursive scanExpr frame while preserving the same classifier and hints.
//
//go:noinline

func (s *globalScoreByteScanner) scanExprRecursive(depth int, loopDepth int, stopAtElse bool) (byte, error) {
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
				term, err := s.scanExprRecursive(depth+1, loopDepth, false)
				if err != nil {
					return 0, err
				}
				if term != 0x0b {
					return term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
			case 0x03: // loop
				term, err := s.scanExprRecursive(depth+1, loopDepth+1, false)
				if err != nil {
					return 0, err
				}
				if term != 0x0b {
					return term, s.r.err(wasm.ErrInvalidInstruction, s.r.off()-1)
				}
			case 0x04: // if
				term, err := s.scanExprRecursive(depth+1, loopDepth, true)
				if err != nil {
					return 0, err
				}
				if term == 0x05 {
					term, err = s.scanExprRecursive(depth+1, loopDepth, false)
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
			term, err := s.scanExprRecursive(depth+1, loopDepth, false)
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
