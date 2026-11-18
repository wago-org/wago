// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"unsafe"
)

const streamMaxNodes = 16384

// admitStream admits only numeric signatures and nontrapping integer bodies.
// Indexed block signatures, loops, arbitrary branches, calls, effects, refs and
// mixed register banks stay on the established function compiler.
func admitStream(code []byte, ft *wasm.CompType, localTypes []wasm.ValType) streamSummary {
	if len(localTypes) < len(ft.Params) {
		return streamSummary{}
	}
	var zeros [2]bool
	for i, t := range localTypes {
		if !streamInteger(t) {
			return streamSummary{}
		}
		if i >= len(ft.Params) {
			if wasm.EqualValType(t, wasm.I64) {
				zeros[1] = true
			} else {
				zeros[0] = true
			}
		}
	}
	return admitScalar(code, ft, len(localTypes), zeros)
}

func streamInteger(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64)
}

// admitStreamFunction reads the existing run-length declarations directly.
// Admission needs only the count and zero widths, not an expanded type array.
func admitStreamFunction(c *wasm.Func, ft *wasm.CompType) streamSummary {
	nLocals := len(ft.Params)
	var zeros [2]bool
	for _, run := range c.Locals.Runs {
		if run.Count == 0 {
			continue // Validated zero-count runs introduce no values or widths.
		}
		if !streamInteger(run.Type) || run.Count > 256 || nLocals > 256-int(run.Count) {
			return streamSummary{}
		}
		nLocals += int(run.Count)
		if run.Count != 0 {
			if wasm.EqualValType(run.Type, wasm.I64) {
				zeros[1] = true
			} else {
				zeros[0] = true
			}
		}
	}
	return admitScalar(c.BodyBytes, ft, nLocals, zeros)
}

func admitScalar(code []byte, ft *wasm.CompType, nLocals int, zeroWidths [2]bool) streamSummary {
	s := streamSummary{Nodes: len(ft.Params) + 1}
	// Preserve the pilot's admission budget independently of storage bounds.
	budget := nLocals + 1
	if len(code) == 0 || len(code) > 64<<10 || nLocals > 256 || len(ft.Results) != 1 {
		return s
	}
	for _, t := range ft.Params {
		if !streamInteger(t) {
			return s
		}
	}
	if !streamInteger(ft.Results[0]) {
		return s
	}
	for _, present := range zeroWidths {
		if present {
			s.Nodes++
		}
	}
	type frame struct {
		base, result  int
		isIf, hasElse bool
	}
	var ctrl [33]frame
	ctrl[0].result = 1
	depth, stack := 1, 0
	r := wasm.ReaderFrom(code)
	for depth > 0 {
		op, e := r.Byte()
		if e != nil {
			return s
		}
		budget++
		if budget > streamMaxNodes {
			return s
		}
		switch op {
		case 0x01:
		case 0x41:
			s.Nodes++
			if _, e = r.I32(); e != nil {
				return s
			}
			stack++
		case 0x42:
			s.Nodes++
			if _, e = r.I64(); e != nil {
				return s
			}
			stack++
		case 0x20, 0x21, 0x22:
			x, e := r.U32()
			if e != nil || int(x) >= nLocals {
				return s
			}
			if op == 0x20 {
				stack++
			} else if op == 0x21 {
				stack--
			}
		case 0x1a:
			stack--
		case 0x02, 0x04:
			budget += nLocals + 2*s.MaxStack
			t, e := r.Byte()
			if e != nil || depth == len(ctrl) {
				return s
			}
			result := 0
			if t == 0x7f || t == 0x7e {
				result = 1
			} else if t != 0x40 {
				return s
			}
			if op == 0x04 {
				s.HasIf = true
				stack--
				s.Nodes += nLocals + stack
			}
			ctrl[depth] = frame{base: stack, result: result, isIf: op == 0x04}
			// Exclude the function frame: CompileStream stores only explicit
			// block/if controls. This fits the existing 32-control bound.
			if depth > int(s.MaxControlDepth) {
				s.MaxControlDepth = uint8(depth)
			}
			depth++
		case 0x05:
			budget += 2 * (nLocals + s.MaxStack)
			f := &ctrl[depth-1]
			if !f.isIf || f.hasElse || stack != f.base+f.result {
				return s
			}
			s.Nodes += 2*nLocals + stack + f.base
			f.hasElse = true
			stack = f.base
		case 0x0b:
			budget += nLocals + s.MaxStack
			f := ctrl[depth-1]
			if stack != f.base+f.result || f.isIf && f.result != 0 && !f.hasElse {
				return s
			}
			if f.isIf {
				s.Nodes += nLocals + stack
			}
			depth--
		case 0x0f:
			// Tail return only: no unreachable polymorphic suffix or inline state.
			if depth != 1 || r.Offset() != len(code)-1 || code[r.Offset()] != 0x0b || stack != 1 {
				return s
			}
		default:
			if _, _, ok := streamOpcode(op); !ok {
				return s
			}
			s.Nodes++
			stack--
		}
		if stack < 0 || stack > 512 {
			return s
		}
		if stack > s.MaxStack {
			s.MaxStack = stack
		}
	}
	if r.Offset() != len(code) {
		return s
	}
	if budget > streamMaxNodes {
		return streamSummary{}
	}
	// The established compiler keeps simple multi-argument leaves in their
	// incoming registers. This pilot currently starts parameters at frame homes;
	// prefer that established lowering when there is no declared local, control,
	// or more than one newly created value to amortize the entry cost. Use the
	// semantic summary rather than byte length, so nop padding cannot bypass it.
	if len(ft.Params) > 1 && nLocals == len(ft.Params) && s.MaxControlDepth == 0 && s.Nodes <= len(ft.Params)+2 {
		return s
	}
	s.Eligible = true
	return s
}

// streamOpcode maps only the admitted nontrapping semantics. Width refers to
// operands; comparisons produce i32.
func streamOpcode(op byte) (shared.IntOp, bool, bool) {
	switch {
	case op >= 0x6a && op <= 0x6c:
		return []shared.IntOp{shared.IntAdd, shared.IntSub, shared.IntMul}[op-0x6a], false, true
	case op >= 0x7c && op <= 0x7e:
		return []shared.IntOp{shared.IntAdd, shared.IntSub, shared.IntMul}[op-0x7c], true, true
	case op >= 0x71 && op <= 0x76:
		return []shared.IntOp{shared.IntAnd, shared.IntOr, shared.IntXor, shared.IntShl, shared.IntShrS, shared.IntShrU}[op-0x71], false, true
	case op >= 0x83 && op <= 0x88:
		return []shared.IntOp{shared.IntAnd, shared.IntOr, shared.IntXor, shared.IntShl, shared.IntShrS, shared.IntShrU}[op-0x83], true, true
	case op >= 0x46 && op <= 0x4f:
		return []shared.IntOp{shared.IntEq, shared.IntNe, shared.IntLtS, shared.IntLtU, shared.IntGtS, shared.IntGtU, shared.IntLeS, shared.IntLeU, shared.IntGeS, shared.IntGeU}[op-0x46], false, true
	case op >= 0x51 && op <= 0x5a:
		return []shared.IntOp{shared.IntEq, shared.IntNe, shared.IntLtS, shared.IntLtU, shared.IntGtS, shared.IntGtU, shared.IntLeS, shared.IntLeU, shared.IntGeS, shared.IntGeU}[op-0x51], true, true
	}
	return shared.IntNone, false, false
}
func streamCompare(op shared.IntOp) bool { return op >= shared.IntEq && op <= shared.IntGeU }

func streamCommutative(op shared.IntOp) bool {
	switch op {
	case shared.IntAdd, shared.IntMul, shared.IntAnd, shared.IntOr, shared.IntXor, shared.IntEq, shared.IntNe:
		return true
	}
	return false
}

// Locations and encoding operands share the established scalar emitter contract.
const (
	streamBorrow   shared.ScalarLocation = 3
	streamDeferred shared.ScalarLocation = 4
)
const streamValueChecks = streamOwnershipChecks || regallocCheckEnabled

type streamID uint32

// Every live value has one authoritative location and a stable arena index.
// An index may be reused only after its last reference and deferred edges die.
// refs includes local owners, logical roots and deferred-parent edges.
type streamNode struct {
	// Tagged payload: the integer for shared.ScalarConstant; for shared.ScalarRegister,
	// 1 marks an incoming i32 carrier whose upper bits are not yet zeroed.
	// Other locations do not consult it. Reusing this word keeps nodes 32B.
	constant          int64
	left, right       streamID
	refs, order       uint16
	slot              int32
	op                shared.IntOp
	kind              shared.ScalarLocation
	reg, depth        uint8
	wide, operandWide bool
	// home is a still-valid canonical copy. The high bit explicitly marks
	// a register home; untagged values retain the local-memory contract.
	// Zero means no copy; it is independent of the authoritative location.
	home uint16
}
type streamControl struct {
	base, result  int
	isIf, hasElse bool
	// One result/local alias, encoded as local index + 1, is enough to
	// preserve a common tee across both incoming edges without a state map.
	resultLocal        uint16
	falseSite, endSite int
}

// streamState holds the common semantic fields. Build-tagged streamState
// definitions keep ordinary type metadata free of checked-only fields. The
// alias shares the field declaration without adding a public type. Its
// pointer-free backing is reused only across functions of one module worker.
type streamState struct {
	bounds                      *streamBoundsCache
	residency                   *streamResidency
	nodes                       []streamNode
	stack, locals               []streamID
	widths                      []bool
	controls                    []streamControl
	freeSlots                   []int
	owners                      [64]streamID
	nextSlot, tempBase, maxSlot int
	regs                        []uint8
	reserved                    uint64
	homeLocals                  [streamMaxHomes]uint16
	homeRegs                    [streamMaxHomes]uint8
	target                      *fn
	flow                        []streamFlow
	flowTypes                   []bool
	dead                        bool
	// Scratch counters are accounting envelopes, not process-memory peaks.
	EventBytes      uint64
	Peak, Discarded uint64
	Spills, Reloads int
}

func (s *streamState) Memory() uint64 {
	return s.boundsBytes() + s.residencyBytes() + s.EventBytes + uint64(cap(s.nodes))*uint64(unsafe.Sizeof(streamNode{})) + uint64(cap(s.stack)+cap(s.locals))*4 + uint64(cap(s.widths)) + uint64(cap(s.controls))*uint64(unsafe.Sizeof(streamControl{})) + uint64(cap(s.freeSlots))*8 + uint64(cap(s.flow))*uint64(unsafe.Sizeof(streamFlow{})) + uint64(cap(s.flowTypes))
}
func (s *streamState) node(id streamID) *streamNode { return &s.nodes[id] }
func (s *streamState) add(n streamNode) streamID {
	if len(s.nodes) != 0 {
		// The sentinel owns creation order and the free-list head. Reusing a
		// dead index never changes the age or location of a live value.
		if streamValueChecks && int(s.nodes[0].order) >= streamMaxNodes {
			panic("streaming operands: admitted creation bound exceeded")
		}
		s.nodes[0].order++
		n.order = s.nodes[0].order
		if id := s.nodes[0].left; id != 0 {
			if streamValueChecks && s.node(id).refs != 0 {
				panic("streaming operands: live value in free list")
			}
			s.nodes[0].left = s.node(id).left
			s.nodes[id] = n

			return id
		}
	}
	if len(s.nodes) == cap(s.nodes) {
		capacity := max(1, 2*cap(s.nodes))
		nodes := make([]streamNode, len(s.nodes), capacity)
		copy(nodes, s.nodes)
		s.Discarded += uint64(cap(s.nodes)) * uint64(unsafe.Sizeof(streamNode{}))
		s.nodes = nodes
	}
	s.nodes = append(s.nodes, n)
	id := streamID(len(s.nodes) - 1)

	return id
}

// Admission bounds all references by two deferred edges per node plus at most
// 256 local bindings and 512 operand roots. That fits uint16 without widening
// streamNode; the unchanged 16,384-node creation budget also bounds its order.
const streamMaxReferences = 2*streamMaxNodes + 256 + 512

func (s *streamState) retain(id streamID) {
	n := s.node(id)
	if streamValueChecks && (id == 0 || n.refs == 0 || int(n.refs) >= streamMaxReferences) {
		panic("streaming operands: invalid retained reference")
	}
	n.refs++
}
func (s *streamState) release(id streamID) {
	if id == 0 {
		return
	}
	n := s.node(id)
	if streamValueChecks && n.refs == 0 {
		panic("streaming operands: released dead value")
	}
	n.refs--
	if n.refs != 0 {
		return
	}
	switch n.kind {
	case streamBacking:
		s.releaseBacking(uint32(n.slot))
	case shared.ScalarRegister:
		s.owners[n.reg] = 0
		if cell := streamRegisterProof(*n); cell != 0 {
			s.releaseBacking(cell)
		}
	case shared.ScalarFrame:
		if int(n.slot) >= s.tempBase {
			s.freeSlots = append(s.freeSlots, int(n.slot))
		}
	case streamDeferred:
		l, r := n.left, n.right
		s.release(l)
		s.release(r)
	}
	// Deferred children are released before their fields become allocator
	// linkage. No local, root or parent may retain this index now.
	n.left = s.nodes[0].left
	s.nodes[0].left = id
}
func (s *streamState) pop() streamID {
	i := len(s.stack) - 1
	id := s.stack[i]
	s.stack = s.stack[:i]
	return id
}
func (s *streamState) operand(id streamID) shared.ScalarOperand {
	n := s.node(id)
	o := shared.ScalarOperand{Kind: n.kind, Reg: n.reg, Constant: n.constant}
	if n.kind == streamBacking {
		return s.backingOperand(uint32(n.slot))
	}
	if n.kind == streamBorrow {
		if r, ok := s.localHomeReg(int(n.slot)); ok {
			o.Kind, o.Reg = shared.ScalarRegister, r
		} else {
			o.Kind = shared.ScalarFrame
			o.Offset = s.target.LocalOffset(int(n.slot))
		}
	} else if n.kind == shared.ScalarFrame {
		o.Offset = s.target.SpillOffset(int(n.slot))
	}
	return o
}
func (s *streamState) slot() int {
	var slot int
	if len(s.freeSlots) > 0 {
		i := len(s.freeSlots) - 1
		slot = s.freeSlots[i]
		s.freeSlots = s.freeSlots[:i]
	} else {
		slot = s.nextSlot
		s.nextSlot++
	}
	if slot+1 > s.maxSlot {
		s.maxSlot = slot + 1
	}
	return slot
}
func (s *streamState) spill(id streamID) {
	n := s.node(id)
	if cell := streamRegisterProof(*n); cell != 0 {
		s.owners[n.reg] = 0
		n.kind, n.slot, n.home = streamBacking, int32(cell), 0
		n.constant, n.depth = 0, 0
		s.target.stats.peep("stream-generation-reuse")
		return
	}
	if n.home != 0 && n.home&streamRegisterHome == 0 {
		// The unchanged local home is already a valid backing copy. Eviction
		// needs no store or temporary slot; identity and references stay intact.
		s.owners[n.reg] = 0
		n.kind = streamBorrow
		n.slot = int32(n.home - 1)
		return
	}
	n.home = 0 // A register-backed copy must never become a stale borrowed home.
	slot := s.slot()
	s.target.Store(s.target.SpillOffset(slot), n.reg, n.wide)
	s.owners[n.reg] = 0
	n.kind = shared.ScalarFrame
	n.slot = int32(slot)
	s.Spills++
}
func (s *streamState) alloc(avoid uint64) uint8 {
	avoid |= s.reserved
	for _, r := range s.regs {
		if avoid&(1<<r) == 0 && s.owners[r] == 0 && (s.residency == nil || s.residency.registers[r] == 0) {
			return r
		}
	}
	var victim streamID
	var victimOrder uint16
	var reg uint8
	protected := true
	for _, r := range s.regs {
		id := s.owners[r]
		if avoid&(1<<r) != 0 || id == 0 {
			continue
		}
		n := s.node(id)
		keep := (n.home != 0 || n.refs > 1) && streamRegisterProof(*n) == 0
		// Creation order increases in bytecode order. Among equally reusable
		// values, evict an older value so recent stack results can begin a
		// reduction in registers. Homes and multiple references suggest reuse;
		// neither is a promise about future uses. This scans only the register
		// bank and leaves active operands and fixed registers excluded above.
		if victim == 0 || protected && !keep || protected == keep && n.order < victimOrder {
			victim, victimOrder, reg, protected = id, n.order, r, keep
		}
	}
	if s.residency != nil {
		// Prefer an unprotected single-reference ordinary owner to evacuating a
		// scored home. Otherwise evacuate the coldest allowed home generation.
		if victim != 0 && !protected {
			s.spill(victim)
			s.target.stats.peep("stream-residency-ordinary-victim")
			return reg
		}
		bank := uint64(0)
		for _, r := range s.regs {
			bank |= uint64(1) << r
		}
		for k := len(s.residency.homes) - 1; k >= 0; k-- {
			cell := s.residency.homes[k]
			if cell == 0 {
				continue
			}
			c := s.residency.cells[cell]
			if c.kind == shared.ScalarRegister && avoid&(1<<c.reg) == 0 && bank&(1<<c.reg) != 0 {
				s.evacuateBacking(cell)
				return c.reg
			}
		}
	}
	if victim != 0 {
		s.spill(victim)
		return reg
	}
	panic("streaming operands: physical register constraints exhausted")
}
func (s *streamState) evict(mask uint64) {
	if s.residency != nil {
		for r, cell := range s.residency.registers {
			if cell != 0 && mask&(1<<r) != 0 {
				s.evacuateBacking(cell)
			}
		}
	}
	for r, id := range s.owners {
		if id != 0 && mask&(1<<r) != 0 {
			s.spill(id)
		}
	}
}
func (s *streamState) materialize(id streamID, avoid uint64) uint8 {

	n := *s.node(id)
	if n.kind == shared.ScalarRegister && avoid&(1<<n.reg) == 0 {
		return n.reg
	}
	if n.kind == streamDeferred {
		return s.expression(id, avoid)
	}
	r := s.alloc(avoid)
	switch n.kind {
	case shared.ScalarConstant:
		s.target.Constant(r, n.constant, n.wide)

	case shared.ScalarRegister:
		s.target.Move(r, n.reg, n.wide)
		s.owners[n.reg] = 0
	case shared.ScalarFrame, streamBorrow, streamBacking:
		o := s.operand(id)
		if o.Kind == shared.ScalarRegister {
			s.target.Move(r, o.Reg, n.wide)
		} else {
			s.target.Load(r, o.Offset, n.wide)
			s.Reloads++
		}
		if n.kind == shared.ScalarFrame && int(n.slot) >= s.tempBase {
			s.freeSlots = append(s.freeSlots, int(n.slot))
		}
	}
	proof := streamRegisterProof(n)
	if n.kind == streamBacking {
		if streamingCopyProofEnabled {
			proof = uint32(n.slot)
			s.target.stats.peep("stream-generation-copy")
		} else {
			s.releaseBacking(uint32(n.slot))
		}
		s.node(id).home = 0
	}
	s.node(id).kind = shared.ScalarRegister
	s.node(id).reg = r
	s.node(id).depth = 0
	s.node(id).constant = int64(uint64(proof) << 1) // moves/loads normalize bit zero, retaining generation identity
	s.owners[r] = id

	return r
}

// At the admitted final return, locals are no longer observable. Drop only
// their owning references before materialization: stack roots and deferred
// edges still keep older local versions alive. This permits an incoming/result
// register to be the expression accumulator without creating a second owner.
func (s *streamState) returnValue(id streamID) {
	s.clearBoundsFacts()
	for i := 0; i < len(s.locals); {
		local := s.locals[i]
		if local == 0 {
			i++
			continue
		}
		end := i + 1
		for end < len(s.locals) && s.locals[end] == local {
			end++
		}
		// Declared-zero pools and local.tee commonly bind the same value to
		// many consecutive locals. Remove those roots with one release, keeping
		// the final release responsible for register/slot/deferred-edge cleanup.
		n := s.node(local)
		count := end - i
		if streamValueChecks && int(n.refs) < count {
			panic("streaming operands: terminal local roots exceed references")
		}
		n.refs -= uint16(count - 1)
		s.release(local)
		i = end
	}
	clear(s.locals)
	reg := s.materialize(id, 0)
	if n := s.node(id); !n.wide && n.constant&1 != 0 {
		// W-register arithmetic already zeroes upper bits. A direct incoming
		// i32 return has no such instruction, so normalize only that carrier.
		s.target.NormalizeI32(reg)
		n.constant &^= 1
	}

	s.target.Return(reg, s.node(id).wide, s.maxSlot)
	s.release(id)
}
func (s *streamState) operands(n streamNode, avoid uint64) (uint8, shared.ScalarOperand) {
	left := s.materialize(n.left, avoid)
	right := s.operand(n.right)
	if !s.target.Operand(n.op, n.operandWide, right) {
		s.materialize(n.right, avoid|1<<left)
		right = s.operand(n.right)
	}
	// A nested fixed-register instruction may have spilled left. Consult its
	// stable identity again instead of reusing the old physical register.
	mask := avoid
	if right.Kind == shared.ScalarRegister {
		mask |= 1 << right.Reg
	}
	if s.node(n.left).kind != shared.ScalarRegister || s.node(n.left).reg != left {
		left = s.materialize(n.left, mask)
	}
	return left, right
}
func (s *streamState) expression(id streamID, avoid uint64) uint8 {

	n := *s.node(id)
	if n.op == shared.IntAdd {
		child := *s.node(n.right)
		if child.kind == streamDeferred && child.op == shared.IntShl && child.refs == 1 {
			count := *s.node(child.right)
			if count.kind == shared.ScalarConstant && count.constant >= 0 && count.constant <= 3 {
				left := s.materialize(n.left, avoid)
				index := s.materialize(child.left, avoid|1<<left)
				if s.node(n.left).kind != shared.ScalarRegister || s.node(n.left).reg != left {
					left = s.materialize(n.left, avoid|1<<index)
				}
				dst := s.alloc(avoid | 1<<left | 1<<index)

				s.forgetRegisterBacking(dst)
				if s.target.ScaledAdd(n.operandWide, dst, left, index, uint8(count.constant)) {

					s.release(n.left)
					s.release(n.right)
					v := s.node(id)
					v.kind = shared.ScalarRegister
					v.reg = dst
					v.depth = 0
					v.constant = 0
					v.left = 0
					v.right = 0
					s.owners[dst] = id
					return dst
				}
			}
		}
	}
	if streamCommutative(n.op) {
		left, right := s.node(n.left), s.node(n.right)
		// Realize the deeper tree first, leaving a spilled/borrowed leaf as
		// the target's optional memory operand. Otherwise prefer a register
		// accumulator over reloading a left-hand memory or constant value.
		if right.depth > left.depth || right.kind == shared.ScalarRegister && left.kind != shared.ScalarRegister && left.kind != streamDeferred {
			n.left, n.right = n.right, n.left
		}
	}
	clobber := s.target.Clobbers(n.op)
	if (n.op == shared.IntShl || n.op == shared.IntShrS || n.op == shared.IntShrU) && s.node(n.right).kind == shared.ScalarConstant {
		clobber = 0 // immediate encoder never reads or writes RCX
	}
	s.evict(clobber)
	avoid |= clobber
	left, right := s.operands(n, avoid)
	dst := left
	if s.node(n.left).refs != 1 {
		mask := avoid | 1<<left
		if right.Kind == shared.ScalarRegister {
			mask |= 1 << right.Reg
		}
		dst = s.alloc(mask)
	}

	s.forgetRegisterBacking(dst)
	s.target.Binary(n.op, n.operandWide, dst, left, right)

	s.release(n.left)
	s.release(n.right)
	value := s.node(id)
	value.kind = shared.ScalarRegister
	value.constant = 0
	value.reg = dst
	value.depth = 0
	value.left = 0
	value.right = 0
	s.owners[dst] = id
	return dst
}
func (s *streamState) binary(op shared.IntOp, wide bool) {
	r, l := s.pop(), s.pop()
	a, b := *s.node(l), *s.node(r)
	resultWide := wide && !streamCompare(op)
	if a.kind == shared.ScalarConstant && b.kind == shared.ScalarConstant {
		var v int64
		if streamCompare(op) {
			v = shared.FoldCompare(op, a.constant, b.constant, wide)
		} else {
			v = shared.FoldBin(op, a.constant, b.constant, wide)
		}
		s.release(l)
		s.release(r)
		s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarConstant, constant: v, wide: resultWide, refs: 1}))
		return
	}
	if a.depth >= 6 {
		s.materialize(l, 0)
		a = *s.node(l)
	}
	if b.depth >= 6 {
		s.materialize(r, 0)
		b = *s.node(r)
	}
	depth := a.depth
	if b.depth > depth {
		depth = b.depth
	}
	depth++
	s.stack = append(s.stack, s.add(streamNode{kind: streamDeferred, op: op, left: l, right: r, wide: resultWide, operandWide: wide, depth: depth, refs: 1}))
}

// canonicalize first captures borrowed and agreement-slot values, then writes
// the new local/stack image. That two-phase ordering prevents alias overwrite.
func (s *streamState) canonicalize(registerResult bool) {
	s.clearBoundsFacts()
	capture := func(id streamID) {
		if id == 0 {
			return
		}
		n := s.node(id)
		if n.kind == streamBorrow && s.locals[n.slot] != id || n.kind == shared.ScalarFrame && int(n.slot) < s.tempBase {
			s.materialize(id, 0)
		}
	}
	for _, id := range s.stack {
		capture(id)
	}
	for _, id := range s.locals {
		capture(id)
	}
	for _, id := range s.stack {
		if s.node(id).kind == streamDeferred {
			s.materialize(id, 0)
		}
	}
	for _, id := range s.locals {
		if id == 0 {
			continue
		}
		if s.node(id).kind == streamDeferred {
			s.materialize(id, 0)
		}
	}
	for i, id := range s.locals {
		if id == 0 {
			continue
		}
		if s.node(id).home == s.localHomeTag(i) {
			continue
		}
		if _, selected := s.localHomeReg(i); selected {
			continue
		}
		r := s.materialize(id, 0)
		s.target.Store(s.target.LocalOffset(i), r, s.node(id).wide)
	}
	var resultReg uint8
	for _, r := range s.regs {
		if (s.reserved|s.backingHomeMask())&(1<<r) == 0 {
			resultReg = r
			break
		}
	}
	for i, id := range s.stack {
		r := s.materialize(id, 0)
		if registerResult && i == len(s.stack)-1 {
			// Both exits select the same physical register. All remaining owners
			// are released by restore; no live logical state needs this register.
			// Stage the result only after home commits, which may allocate.
			continue
		}
		s.target.Store(s.target.SpillOffset(i+1), r, s.node(id).wide)
		if i+2 > s.maxSlot {
			s.maxSlot = i + 2
		}
	}

	s.commitRegisterHomes()
	savedHomes := s.reserved
	s.reserved |= s.backingHomeMask()
	defer func() { s.reserved = savedHomes }()
	if registerResult {
		id := s.stack[len(s.stack)-1]
		r := s.materialize(id, 0)
		s.target.Move(resultReg, r, s.node(id).wide)
	}
	s.restore(len(s.stack))
	if registerResult {
		id := s.stack[len(s.stack)-1]
		s.node(id).kind = shared.ScalarRegister
		s.node(id).reg = resultReg
		s.owners[resultReg] = id
	}
}

// resultLocal records an identity relation, never a physical register. The
// second edge must independently prove the same relation before it survives.
func (s *streamState) resultLocal() uint16 {
	id := s.stack[len(s.stack)-1]
	for i, local := range s.locals {
		if local == id {
			return uint16(i + 1)
		}
	}
	return 0
}

func (s *streamState) restoreResultLocal(local uint16) {
	id := s.stack[len(s.stack)-1]
	s.release(s.locals[local-1])
	s.retain(id)
	s.locals[local-1] = id
	// canonicalize wrote this home on each incoming edge.
	s.node(id).home = s.localHomeTag(int(local) - 1)
}

func (s *streamState) restore(depth int) {
	s.clearBoundsFacts()

	// Stack types are retained through the agreement. Each else restores only the
	// pre-split prefix, whose types cannot change in a validated scalar body.
	for _, id := range s.locals {
		s.release(id)
	}
	for i, id := range s.stack {
		wide := s.node(id).wide
		s.release(id)
		if i < depth {
			s.stack[i] = s.add(streamNode{kind: shared.ScalarFrame, slot: int32(i + 1), wide: wide, refs: 1})
		}
	}
	s.stack = s.stack[:depth]
	for i, wide := range s.widths {
		if s.locals[i] == 0 {
			continue
		}
		if s.residency != nil {
			found := false
			for k, n := range s.homeLocals {
				if n != 0 && int(n)-1 == i {
					s.locals[i] = s.borrowBacking(s.residency.homes[k], i)
					found = true
					break
				}
			}
			if found {
				continue
			}
		}
		s.locals[i] = s.add(streamNode{kind: streamBorrow, slot: int32(i), home: s.localHomeTag(i), wide: wide, refs: 1})
	}

}

func (s *streamState) CompileStream(code []byte, summary streamSummary, localWide []bool, nParams int, target *fn) (int, error) {
	return s.compileStream(code, summary, localWide, nParams, target)
}

func (s *streamState) compileStream(code []byte, summary streamSummary, localWide []bool, nParams int, target *fn) (int, error) {
	if !summary.Eligible {
		return 0, fmt.Errorf("streaming operands: unadmitted function")
	}

	// Reset certificates before resetting the sentinel creation order. Allocate
	// one bounded scratch object per worker, never per access or value.
	if streamingBoundsFactsEnabled {
		if summary.Memory && s.bounds == nil {
			s.bounds = &streamBoundsCache{}
		}
		s.clearBoundsFacts()
	} else {
		s.Discarded += s.boundsBytes()
		s.bounds = nil
	}
	s.EventBytes = uint64(cap(summary.LocalEvents)) * 4
	old := s.Memory()
	if cap(s.nodes) > 4096 && summary.Nodes < cap(s.nodes)/4 {
		s.nodes = nil
		s.Discarded += old - s.Memory()
	}
	// The summary bounds total creation events, not simultaneous live values.
	// Start small and grow geometrically only when live references require it.
	if needed := min(summary.Nodes, 64); cap(s.nodes) < needed {
		s.Discarded += uint64(cap(s.nodes)) * uint64(unsafe.Sizeof(streamNode{}))
		s.nodes = make([]streamNode, 0, needed)
	}
	if streamingResidencyEnabled && summary.HomeLocals != ([streamMaxHomes]uint16{}) {
		if s.residency == nil {
			s.residency = &streamResidency{}
		}
		r := s.residency
		r.cells = r.cells[:0]
		r.cells = append(r.cells, streamBackingCell{})
		r.free = 0
		r.homes = [streamMaxHomes]uint32{}
		r.registers = [64]uint32{}
	} else {
		s.residency = nil
	}
	s.nodes = s.nodes[:0]
	s.add(streamNode{})
	if cap(s.stack) < summary.MaxStack {
		s.stack = make([]streamID, 0, summary.MaxStack)
	}
	if cap(s.locals) < len(localWide) {
		s.locals = make([]streamID, 0, len(localWide))
	}
	s.stack = s.stack[:0]
	s.locals = s.locals[:0]
	s.widths = append(s.widths[:0], localWide...)
	if needed := int(summary.MaxControlDepth); cap(s.controls) < needed {
		capacity := max(needed, 2*cap(s.controls))
		capacity = min(capacity, 32)
		s.controls = make([]streamControl, 0, capacity)
	}
	s.controls = s.controls[:0]
	s.freeSlots = s.freeSlots[:0]
	s.owners = [64]streamID{}
	// Plain blocks have only fallthrough. Without an if, temporary spills may
	// use slot zero: return materializes its only result before overwriting
	// that slot, and no local or operand is consumed afterward. Conditional
	// agreements retain their disjoint operand-home range.
	s.tempBase = 0
	if summary.HasIf {
		s.tempBase = summary.MaxStack + 1
	}
	s.nextSlot = s.tempBase
	s.maxSlot = 0
	s.Spills = 0
	s.Reloads = 0
	s.target = target
	s.regs, s.reserved = target.Registers()
	s.homeLocals = [streamMaxHomes]uint16{}
	s.homeRegs = [streamMaxHomes]uint8{}
	var zeros [2]streamID
	for i, wide := range localWide {
		if i < nParams {
			n := streamNode{kind: streamBorrow, wide: wide, refs: 1, slot: int32(i), home: uint16(i + 1)}
			reg, incoming := target.ParameterRegister(i)
			if incoming {
				if streamValueChecks && (reg >= 64 || s.reserved&(1<<reg) != 0 || s.owners[reg] != 0) {
					panic("streaming operands: invalid incoming parameter register")
				}
				n.kind, n.reg, n.home = shared.ScalarRegister, reg, 0
				if !wide {
					n.constant = 1
				}
			}
			id := s.add(n)
			s.locals = append(s.locals, id)

			if incoming {
				s.owners[reg] = id
			}
			continue
		}
		width := 0
		if wide {
			width = 1
		}
		id := zeros[width]
		if id == 0 {
			id = s.add(streamNode{kind: shared.ScalarConstant, wide: wide, refs: 1})
			zeros[width] = id
		} else {
			s.retain(id)
		}
		s.locals = append(s.locals, id)
	}
	s.initializeRegisterHomes(summary.HomeLocals, nParams)
	return s.runStreamCode(code, summary)
}

// FinishWorker releases shared scratch after the worker's last function.
func (s *streamState) FinishWorker() {

	s.Discarded += s.Memory()
	s.bounds = nil
	s.EventBytes = 0
	s.residency = nil
	s.nodes = nil
	s.stack = nil
	s.locals = nil
	s.widths = nil
	s.controls = nil
	s.flow = nil
	s.flowTypes = nil
	s.freeSlots = nil
	s.target = nil
	s.regs = nil
}
