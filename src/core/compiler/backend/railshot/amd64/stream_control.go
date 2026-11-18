//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A header agreement is distinct from value identity. The first implementation
// selects canonical frame homes. Stable GPR homes and parallel edge transfers
// can extend this seam without phi nodes or changing refcounted local snapshots.
type streamFlow struct {
	kind                           byte
	base, result, typeStart        int
	wide, entryDead, ends, hasElse bool
	head, falseSite                int
	endHead                        uint32
}

func (s *streamState) streamRestore(fr streamFlow, result bool) {
	for _, id := range s.locals {
		s.release(id)
	}
	for _, id := range s.stack {
		s.release(id)
	}
	s.stack = s.stack[:0]
	for i, w := range s.flowTypes[fr.typeStart : fr.typeStart+fr.base] {
		s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarFrame, slot: int32(i + 1), wide: w, refs: 1}))
	}
	if result && fr.result != 0 {
		s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarFrame, slot: int32(fr.base + 1), wide: fr.wide, refs: 1}))
	}
	for i, w := range s.widths {
		s.locals[i] = s.add(streamNode{kind: streamBorrow, slot: int32(i), home: uint16(i + 1), wide: w, refs: 1})
	}
}
func (s *streamState) streamLink(fr *streamFlow) {
	site := s.target.Jump()
	if site < 0 || uint64(site) >= uint64(^uint32(0)) || site+4 > len(s.target.a.B) {
		s.target.setRepresentationLimit(functionRepresentationFrameEnd)
		return
	}
	binary.LittleEndian.PutUint32(s.target.a.B[site:site+4], fr.endHead)
	fr.endHead = uint32(site + 1)
}
func (s *streamState) streamPatch(fr streamFlow) {
	for h := fr.endHead; h != 0; {
		site := int(h - 1)
		h = binary.LittleEndian.Uint32(s.target.a.B[site : site+4])
		s.target.a.PatchRel32(site, s.target.a.Len())
	}
}
func (s *streamState) streamCaptureCondition(id streamID) {
	n := *s.node(id)
	if n.kind == streamDeferred && streamCompare(n.op) {
		s.materialize(n.left, 0)
		s.materialize(n.right, 0)
	} else {
		s.materialize(id, 0)
	}
}

// Transfer executes only on the taken edge. Conditional fallthrough restores
// the pre-edge canonical image without emitting stores from the taken path.
func (s *streamState) streamEdge(depth int, conditional bool) {
	target := len(s.flow) - 1 - depth
	var condition streamID
	if conditional {
		condition = s.pop()
		s.streamCaptureCondition(condition)
	}
	s.canonicalize(false)
	falseSite := -1
	if conditional {
		falseSite = s.condition(condition)
	}
	fr := &s.flow[target]
	if fr.kind != 0x03 && fr.result != 0 {
		id := s.stack[len(s.stack)-1]
		r := s.materialize(id, 0)
		s.target.Store(s.target.SpillOffset(fr.base+1), r, s.node(id).wide)
		s.maxSlot = max(s.maxSlot, fr.base+2)
	}
	if fr.kind == 0x03 {
		site := s.target.Jump()
		s.target.a.PatchRel32(site, fr.head)
	} else {
		s.streamLink(fr)
		fr.ends = true
	}
	if conditional {
		s.target.a.PatchRel32(falseSite, s.target.a.Len())
		s.restore(len(s.stack))
	} else {
		s.dead = true
	}
}
func (s *streamState) runStreamCode(code []byte, summary streamSummary) (int, error) {
	s.flow = s.flow[:0]
	s.flowTypes = s.flowTypes[:0]
	s.dead = false
	s.flow = append(s.flow, streamFlow{kind: 0xff, result: summary.ResultN, wide: summary.ResultWide, falseSite: -1})
	r := wasm.ReaderFrom(code)
	for len(s.flow) > 0 {
		s.target.wasmPC = s.target.tracePCBase + uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			return 0, err
		}
		// Structural operations remain visible through unreachable syntax; operand
		// operations are skipped without publishing values or emitting instructions.
		if s.dead && op != 0x02 && op != 0x03 && op != 0x04 && op != 0x05 && op != 0x0b {
			if e := wasm.SkipInstructionImmediate(&r, op); e != nil {
				return 0, e
			}
			continue
		}
		switch op {
		case 0x00:
			s.target.trapAlways(trapUnreachable)
			s.dead = true
		case 0x01:
		case 0x41:
			v, e := r.I32()
			if e != nil {
				return 0, e
			}
			s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarConstant, constant: int64(v), refs: 1}))
		case 0x42:
			v, e := r.I64()
			if e != nil {
				return 0, e
			}
			s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarConstant, constant: v, wide: true, refs: 1}))
		case 0x20, 0x21, 0x22:
			x, e := r.U32()
			if e != nil {
				return 0, e
			}
			if op == 0x20 {
				id := s.locals[x]
				s.retain(id)
				s.stack = append(s.stack, id)
			} else {
				id := s.pop()
				old := s.locals[x]
				if old != id && s.node(old).home == uint16(x+1) {
					s.node(old).home = 0
				}
				s.release(old)
				if s.node(id).kind != shared.ScalarConstant {
					s.materialize(id, 0)
				}
				s.locals[x] = id
				if op == 0x22 {
					s.retain(id)
					s.stack = append(s.stack, id)
				}
			}
		case 0x1a:
			s.release(s.pop())
		case 0x02, 0x03, 0x04:
			typ, e := r.Byte()
			if e != nil {
				return 0, e
			}
			fr := streamFlow{kind: op, entryDead: s.dead, falseSite: -1, wide: typ == 0x7e}
			if typ != 0x40 {
				fr.result = 1
			}
			var cond streamID
			if !s.dead {
				if op == 0x04 {
					cond = s.pop()
					s.streamCaptureCondition(cond)
				}
				s.canonicalize(false)
				if op == 0x04 {
					fr.falseSite = s.condition(cond)
				}
			}
			fr.base = len(s.stack)
			fr.typeStart = len(s.flowTypes)
			for _, id := range s.stack {
				s.flowTypes = append(s.flowTypes, s.node(id).wide)
			}
			if op == 0x03 && !s.dead {
				if s.target.compactLoopAlign32 {
					s.target.a.AlignLoop32()
				} else {
					s.target.a.AlignLoop()
				}
				fr.head = s.target.a.Len()
				// Canonical ownership is empty here. RSI is never a live stream owner
				// across this header poll; future register agreements must preserve that.
				s.target.emitInterruptCheck(RSI)
			}
			s.flow = append(s.flow, fr)
		case 0x05:
			i := len(s.flow) - 1
			fr := &s.flow[i]
			if !s.dead {
				s.canonicalize(false)
				s.streamLink(fr)
				fr.ends = true
			}
			if fr.falseSite >= 0 {
				s.target.a.PatchRel32(fr.falseSite, s.target.a.Len())
			}
			fr.hasElse = true
			s.dead = fr.entryDead
			if !s.dead {
				s.streamRestore(*fr, false)
			}
		case 0x0b:
			i := len(s.flow) - 1
			fr := s.flow[i]
			fallthroughLive := !s.dead
			live := fallthroughLive || fr.ends || (fr.kind == 0x04 && !fr.hasElse && !fr.entryDead)
			if fallthroughLive {
				s.canonicalize(false)
			}
			if fr.kind == 0x04 && !fr.hasElse && fr.falseSite >= 0 {
				s.target.a.PatchRel32(fr.falseSite, s.target.a.Len())
			}
			s.streamPatch(fr)
			// A live fallthrough is already in exactly this canonical shape.
			// Only a branch-only join needs its compile-time image reconstructed.
			if live && !fallthroughLive {
				s.streamRestore(fr, true)
			}
			s.flow = s.flow[:i]
			s.flowTypes = s.flowTypes[:fr.typeStart]
			s.dead = !live
			if i == 0 {
				if live && fr.result != 0 {
					s.returnValue(s.pop())
				}
				s.target = nil
				s.regs = nil
				if m := s.Memory(); m > s.Peak {
					s.Peak = m
				}
				return s.maxSlot, nil
			}
		case 0x0c, 0x0d:
			depth, e := r.U32()
			if e != nil {
				return 0, e
			}
			s.streamEdge(int(depth), op == 0x0d)
		case 0x0f:
			s.streamEdge(len(s.flow)-1, false)
		case 0xfc:
			sub, e := r.U32()
			if e != nil {
				return 0, e
			}
			if e = s.bulk(&r, sub); e != nil {
				return 0, e
			}
		default:
			if size, _, _, _ := streamMemoryShape(op); size != 0 {
				if e := s.memory(&r, op); e != nil {
					return 0, e
				}
			} else if operation, wide, ok := streamOpcode(op); ok {
				s.binary(operation, wide)
			} else {
				return 0, fmt.Errorf("stream: admission mismatch opcode %#x", op)
			}
		}
	}
	return 0, fmt.Errorf("stream: missing function end")
}
