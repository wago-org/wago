package shared

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"unsafe"
)

// ScalarSummary is admission metadata, not an instruction array. Admission is
// performed on validated bytecode before any function instruction is emitted.
type ScalarSummary struct {
	Eligible        bool
	MaxStack, Nodes int
}

const scalarMaxNodes = 16384

// AdmitScalar admits only numeric signatures and nontrapping integer bodies.
// Indexed block signatures, loops, arbitrary branches, calls, effects, refs and
// mixed register banks stay on the established function compiler.
func AdmitScalar(code []byte, ft *wasm.CompType, localTypes []wasm.ValType) ScalarSummary {
	s := ScalarSummary{Nodes: len(localTypes) + 1}
	if len(code) == 0 || len(code) > 64<<10 || len(localTypes) > 256 || len(ft.Results) != 1 {
		return s
	}
	integer := func(t wasm.ValType) bool { return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64) }
	for _, t := range ft.Params {
		if !integer(t) {
			return s
		}
	}
	if !integer(ft.Results[0]) {
		return s
	}
	for _, t := range localTypes {
		if !integer(t) {
			return s
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
		s.Nodes++
		if s.Nodes > scalarMaxNodes {
			return s
		}
		switch op {
		case 0x01:
		case 0x41:
			if _, e = r.I32(); e != nil {
				return s
			}
			stack++
		case 0x42:
			if _, e = r.I64(); e != nil {
				return s
			}
			stack++
		case 0x20, 0x21, 0x22:
			x, e := r.U32()
			if e != nil || int(x) >= len(localTypes) {
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
			s.Nodes += len(localTypes) + 2*s.MaxStack
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
				stack--
			}
			ctrl[depth] = frame{base: stack, result: result, isIf: op == 0x04}
			depth++
		case 0x05:
			s.Nodes += 2 * (len(localTypes) + s.MaxStack)
			f := &ctrl[depth-1]
			if !f.isIf || f.hasElse || stack != f.base+f.result {
				return s
			}
			f.hasElse = true
			stack = f.base
		case 0x0b:
			s.Nodes += len(localTypes) + s.MaxStack
			f := ctrl[depth-1]
			if stack != f.base+f.result || f.isIf && f.result != 0 && !f.hasElse {
				return s
			}
			depth--
		case 0x0f:
			// Tail return only: no unreachable polymorphic suffix or inline state.
			if depth != 1 || r.Offset() != len(code)-1 || code[r.Offset()] != 0x0b || stack != 1 {
				return s
			}
		default:
			if _, _, ok := ScalarOpcode(op); !ok {
				return s
			}
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
	if s.Nodes > scalarMaxNodes {
		return ScalarSummary{}
	}
	s.Eligible = true
	return s
}

// ScalarOpcode maps only the admitted nontrapping semantics. Width refers to
// operands; comparisons produce i32.
func ScalarOpcode(op byte) (IntOp, bool, bool) {
	switch {
	case op >= 0x6a && op <= 0x6c:
		return []IntOp{IntAdd, IntSub, IntMul}[op-0x6a], false, true
	case op >= 0x7c && op <= 0x7e:
		return []IntOp{IntAdd, IntSub, IntMul}[op-0x7c], true, true
	case op >= 0x71 && op <= 0x76:
		return []IntOp{IntAnd, IntOr, IntXor, IntShl, IntShrS, IntShrU}[op-0x71], false, true
	case op >= 0x83 && op <= 0x88:
		return []IntOp{IntAnd, IntOr, IntXor, IntShl, IntShrS, IntShrU}[op-0x83], true, true
	case op >= 0x46 && op <= 0x4f:
		return []IntOp{IntEq, IntNe, IntLtS, IntLtU, IntGtS, IntGtU, IntLeS, IntLeU, IntGeS, IntGeU}[op-0x46], false, true
	case op >= 0x51 && op <= 0x5a:
		return []IntOp{IntEq, IntNe, IntLtS, IntLtU, IntGtS, IntGtU, IntLeS, IntLeU, IntGeS, IntGeU}[op-0x51], true, true
	}
	return IntNone, false, false
}
func scalarCompare(op IntOp) bool { return op >= IntEq && op <= IntGeU }

type ScalarLocation uint8

const (
	ScalarConstant ScalarLocation = iota
	ScalarRegister
	ScalarFrame
	scalarBorrow
	scalarDeferred
)

// ScalarOperand is a temporary physical description. Value identity is an arena
// index owned by ScalarState, and never a register number or frame address.
type ScalarOperand struct {
	Kind     ScalarLocation
	Reg      uint8
	Offset   int32
	Constant int64
}

// ScalarTarget supplies physical constraints and encoding only. It must not
// retain operands or maintain logical stack, local, control or allocator state.
type ScalarTarget interface {
	Registers() ([]uint8, uint64)
	LocalOffset(int) int32
	SpillOffset(int) int32
	Constant(uint8, int64, bool)
	Move(uint8, uint8, bool)
	Load(uint8, int32, bool)
	Store(int32, uint8, bool)
	Clobbers(IntOp) uint64
	Operand(IntOp, bool, ScalarOperand) bool
	Binary(IntOp, bool, uint8, uint8, ScalarOperand)
	ScaledAdd(bool, uint8, uint8, uint8, uint8) bool
	BranchZero(uint8) int
	BranchCompare(IntOp, bool, uint8, ScalarOperand) int
	Jump() int
	Patch(int, int) error
	Position() int
	Return(uint8, bool, int)
}

type scalarID uint32

// Every node has exactly one authoritative location and a stable identity.
// refs includes local owners, logical roots and deferred-parent edges.
type scalarNode struct {
	constant          int64
	left, right       scalarID
	refs              int32
	slot              int32
	op                IntOp
	kind              ScalarLocation
	reg, depth        uint8
	wide, operandWide bool
}
type scalarControl struct {
	base, result       int
	isIf, hasElse      bool
	falseSite, endSite int
}

// ScalarState contains the sole semantic state for an admitted function. Its
// pointer-free backing is reused only across functions of one module worker.
type ScalarState struct {
	nodes                       []scalarNode
	stack, locals               []scalarID
	widths                      []bool
	controls                    []scalarControl
	freeSlots                   []int
	owners                      [64]scalarID
	nextSlot, tempBase, maxSlot int
	regs                        []uint8
	reserved                    uint64
	target                      ScalarTarget
	// Scratch counters are accounting envelopes, not process-memory peaks.
	Peak, Discarded uint64
	Spills, Reloads int
}

func (s *ScalarState) Memory() uint64 {
	return uint64(cap(s.nodes))*uint64(unsafe.Sizeof(scalarNode{})) + uint64(cap(s.stack)+cap(s.locals))*4 + uint64(cap(s.widths)) + uint64(cap(s.controls))*uint64(unsafe.Sizeof(scalarControl{})) + uint64(cap(s.freeSlots))*8
}
func (s *ScalarState) node(id scalarID) *scalarNode { return &s.nodes[id] }
func (s *ScalarState) add(n scalarNode) scalarID {
	s.nodes = append(s.nodes, n)
	return scalarID(len(s.nodes) - 1)
}
func (s *ScalarState) release(id scalarID) {
	if id == 0 {
		return
	}
	n := s.node(id)
	n.refs--
	if n.refs != 0 {
		return
	}
	switch n.kind {
	case ScalarRegister:
		s.owners[n.reg] = 0
	case ScalarFrame:
		if int(n.slot) >= s.tempBase {
			s.freeSlots = append(s.freeSlots, int(n.slot))
		}
	case scalarDeferred:
		l, r := n.left, n.right
		s.release(l)
		s.release(r)
	}
}
func (s *ScalarState) pop() scalarID {
	i := len(s.stack) - 1
	id := s.stack[i]
	s.stack = s.stack[:i]
	return id
}
func (s *ScalarState) operand(id scalarID) ScalarOperand {
	n := s.node(id)
	o := ScalarOperand{Kind: n.kind, Reg: n.reg, Constant: n.constant}
	if n.kind == scalarBorrow {
		o.Kind = ScalarFrame
		o.Offset = s.target.LocalOffset(int(n.slot))
	} else if n.kind == ScalarFrame {
		o.Offset = s.target.SpillOffset(int(n.slot))
	}
	return o
}
func (s *ScalarState) slot() int {
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
func (s *ScalarState) spill(id scalarID) {
	n := s.node(id)
	slot := s.slot()
	s.target.Store(s.target.SpillOffset(slot), n.reg, n.wide)
	s.owners[n.reg] = 0
	n.kind = ScalarFrame
	n.slot = int32(slot)
	s.Spills++
}
func (s *ScalarState) alloc(avoid uint64) uint8 {
	avoid |= s.reserved
	for _, r := range s.regs {
		if avoid&(1<<r) == 0 && s.owners[r] == 0 {
			return r
		}
	}
	for _, r := range s.regs {
		if avoid&(1<<r) == 0 && s.owners[r] != 0 {
			s.spill(s.owners[r])
			return r
		}
	}
	panic("shared scalar: physical register constraints exhausted")
}
func (s *ScalarState) evict(mask uint64) {
	for r, id := range s.owners {
		if id != 0 && mask&(1<<r) != 0 {
			s.spill(id)
		}
	}
}
func (s *ScalarState) materialize(id scalarID, avoid uint64) uint8 {
	n := *s.node(id)
	if n.kind == ScalarRegister && avoid&(1<<n.reg) == 0 {
		return n.reg
	}
	if n.kind == scalarDeferred {
		return s.expression(id, avoid)
	}
	r := s.alloc(avoid)
	switch n.kind {
	case ScalarConstant:
		s.target.Constant(r, n.constant, n.wide)
	case ScalarRegister:
		s.target.Move(r, n.reg, n.wide)
		s.owners[n.reg] = 0
	case ScalarFrame, scalarBorrow:
		s.target.Load(r, s.operand(id).Offset, n.wide)
		s.Reloads++
		if n.kind == ScalarFrame && int(n.slot) >= s.tempBase {
			s.freeSlots = append(s.freeSlots, int(n.slot))
		}
	}
	s.node(id).kind = ScalarRegister
	s.node(id).reg = r
	s.node(id).depth = 0
	s.owners[r] = id
	return r
}
func (s *ScalarState) operands(n scalarNode, avoid uint64) (uint8, ScalarOperand) {
	left := s.materialize(n.left, avoid)
	right := s.operand(n.right)
	if !s.target.Operand(n.op, n.operandWide, right) {
		s.materialize(n.right, avoid|1<<left)
		right = s.operand(n.right)
	}
	// A nested fixed-register instruction may have spilled left. Consult its
	// stable identity again instead of reusing the old physical register.
	mask := avoid
	if right.Kind == ScalarRegister {
		mask |= 1 << right.Reg
	}
	if s.node(n.left).kind != ScalarRegister || s.node(n.left).reg != left {
		left = s.materialize(n.left, mask)
	}
	return left, right
}
func (s *ScalarState) expression(id scalarID, avoid uint64) uint8 {
	n := *s.node(id)
	if n.op == IntAdd {
		child := *s.node(n.right)
		if child.kind == scalarDeferred && child.op == IntShl && child.refs == 1 {
			count := *s.node(child.right)
			if count.kind == ScalarConstant && count.constant >= 0 && count.constant <= 3 {
				left := s.materialize(n.left, avoid)
				index := s.materialize(child.left, avoid|1<<left)
				if s.node(n.left).kind != ScalarRegister || s.node(n.left).reg != left {
					left = s.materialize(n.left, avoid|1<<index)
				}
				dst := s.alloc(avoid | 1<<left | 1<<index)
				if s.target.ScaledAdd(n.operandWide, dst, left, index, uint8(count.constant)) {
					s.release(n.left)
					s.release(n.right)
					v := s.node(id)
					v.kind = ScalarRegister
					v.reg = dst
					v.depth = 0
					v.left = 0
					v.right = 0
					s.owners[dst] = id
					return dst
				}
			}
		}
	}
	clobber := s.target.Clobbers(n.op)
	s.evict(clobber)
	avoid |= clobber
	left, right := s.operands(n, avoid)
	dst := left
	if s.node(n.left).refs != 1 {
		mask := avoid | 1<<left
		if right.Kind == ScalarRegister {
			mask |= 1 << right.Reg
		}
		dst = s.alloc(mask)
	}
	s.target.Binary(n.op, n.operandWide, dst, left, right)
	s.release(n.left)
	s.release(n.right)
	value := s.node(id)
	value.kind = ScalarRegister
	value.reg = dst
	value.depth = 0
	value.left = 0
	value.right = 0
	s.owners[dst] = id
	return dst
}
func (s *ScalarState) binary(op IntOp, wide bool) {
	r, l := s.pop(), s.pop()
	a, b := *s.node(l), *s.node(r)
	resultWide := wide && !scalarCompare(op)
	if a.kind == ScalarConstant && b.kind == ScalarConstant {
		var v int64
		if scalarCompare(op) {
			v = FoldCompare(op, a.constant, b.constant, wide)
		} else {
			v = FoldBin(op, a.constant, b.constant, wide)
		}
		s.release(l)
		s.release(r)
		s.stack = append(s.stack, s.add(scalarNode{kind: ScalarConstant, constant: v, wide: resultWide, refs: 1}))
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
	s.stack = append(s.stack, s.add(scalarNode{kind: scalarDeferred, op: op, left: l, right: r, wide: resultWide, operandWide: wide, depth: depth, refs: 1}))
}

// canonicalize first captures borrowed and agreement-slot values, then writes
// the new local/stack image. That two-phase ordering prevents alias overwrite.
func (s *ScalarState) canonicalize() {
	capture := func(id scalarID) {
		n := s.node(id)
		if n.kind == scalarBorrow || n.kind == ScalarFrame && int(n.slot) < s.tempBase {
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
		if s.node(id).kind == scalarDeferred {
			s.materialize(id, 0)
		}
	}
	for _, id := range s.locals {
		if s.node(id).kind == scalarDeferred {
			s.materialize(id, 0)
		}
	}
	for i, id := range s.locals {
		r := s.materialize(id, 0)
		s.target.Store(s.target.LocalOffset(i), r, s.node(id).wide)
	}
	for i, id := range s.stack {
		r := s.materialize(id, 0)
		s.target.Store(s.target.SpillOffset(i+1), r, s.node(id).wide)
		if i+2 > s.maxSlot {
			s.maxSlot = i + 2
		}
	}
	s.restore(len(s.stack))
}
func (s *ScalarState) restore(depth int) {
	// Stack types are retained through the agreement. Each else restores only the
	// pre-split prefix, whose types cannot change in a validated scalar body.
	for _, id := range s.locals {
		s.release(id)
	}
	for i, id := range s.stack {
		wide := s.node(id).wide
		s.release(id)
		if i < depth {
			s.stack[i] = s.add(scalarNode{kind: ScalarFrame, slot: int32(i + 1), wide: wide, refs: 1})
		}
	}
	s.stack = s.stack[:depth]
	for i, wide := range s.widths {
		s.locals[i] = s.add(scalarNode{kind: scalarBorrow, slot: int32(i), wide: wide, refs: 1})
	}
}
func (s *ScalarState) condition(id scalarID) int {
	n := *s.node(id)
	if n.kind == scalarDeferred && scalarCompare(n.op) {
		left, right := s.operands(n, 0)
		site := s.target.BranchCompare(n.op, n.operandWide, left, right)
		s.release(id)
		return site
	}
	reg := s.materialize(id, 0)
	site := s.target.BranchZero(reg)
	s.release(id)
	return site
}

// CompileScalar consumes bytecode directly with bounded deferred expression
// trees. It assumes the caller validated and admitted the same immutable bytes.
func (s *ScalarState) CompileScalar(code []byte, summary ScalarSummary, localWide []bool, nParams int, target ScalarTarget) (int, error) {
	if !summary.Eligible {
		return 0, fmt.Errorf("shared scalar: unadmitted function")
	}
	old := s.Memory()
	if cap(s.nodes) > 4096 && summary.Nodes < cap(s.nodes)/4 {
		s.nodes = nil
		s.Discarded += old - s.Memory()
	}
	if cap(s.nodes) < summary.Nodes {
		s.nodes = make([]scalarNode, 0, summary.Nodes)
	}
	s.nodes = s.nodes[:0]
	s.add(scalarNode{})
	s.stack = s.stack[:0]
	s.locals = s.locals[:0]
	s.widths = append(s.widths[:0], localWide...)
	s.controls = s.controls[:0]
	s.freeSlots = s.freeSlots[:0]
	s.owners = [64]scalarID{}
	s.tempBase = summary.MaxStack + 1
	s.nextSlot = s.tempBase
	s.maxSlot = 0
	s.Spills = 0
	s.Reloads = 0
	s.target = target
	s.regs, s.reserved = target.Registers()
	for i, wide := range localWide {
		n := scalarNode{kind: ScalarConstant, wide: wide, refs: 1}
		if i < nParams {
			n.kind = scalarBorrow
			n.slot = int32(i)
		}
		s.locals = append(s.locals, s.add(n))
	}
	r := wasm.ReaderFrom(code)
	returned := false
	for {
		op, err := r.Byte()
		if err != nil {
			return 0, err
		}
		switch op {
		case 0x01:
		case 0x41:
			v, e := r.I32()
			if e != nil {
				return 0, e
			}
			s.stack = append(s.stack, s.add(scalarNode{kind: ScalarConstant, constant: int64(v), refs: 1}))
		case 0x42:
			v, e := r.I64()
			if e != nil {
				return 0, e
			}
			s.stack = append(s.stack, s.add(scalarNode{kind: ScalarConstant, constant: v, wide: true, refs: 1}))
		case 0x20, 0x21, 0x22:
			x, e := r.U32()
			if e != nil {
				return 0, e
			}
			if op == 0x20 {
				id := s.locals[x]
				s.node(id).refs++
				s.stack = append(s.stack, id)
			} else {
				id := s.pop()
				// Release the overwritten binding before realization. Older stack reads
				// and deferred edges retain their own references, so only an unborrowed
				// source becomes available for in-place lowering at this sink.
				s.release(s.locals[x])
				s.materialize(id, 0)
				s.locals[x] = id
				if op == 0x22 {
					s.node(id).refs++
					s.stack = append(s.stack, id)
				}
			}
		case 0x1a:
			s.release(s.pop())
		case 0x02, 0x04:
			t, e := r.Byte()
			if e != nil {
				return 0, e
			}
			result := 0
			if t != 0x40 {
				result = 1
			}
			fr := scalarControl{result: result, isIf: op == 0x04, falseSite: -1, endSite: -1}
			var cond scalarID
			if fr.isIf {
				cond = s.pop()
				n := *s.node(cond)
				if n.kind == scalarDeferred && scalarCompare(n.op) {
					s.materialize(n.left, 0)
					s.materialize(n.right, 0)
				} else {
					s.materialize(cond, 0)
				}
			}
			fr.base = len(s.stack)
			if fr.isIf {
				s.canonicalize()
				fr.falseSite = s.condition(cond)
			}
			// Admission excludes br/br_if/br_table. A plain block therefore has
			// only fallthrough edges and needs no physical state agreement.
			s.controls = append(s.controls, fr)
		case 0x05:
			i := len(s.controls) - 1
			fr := &s.controls[i]
			s.canonicalize()
			fr.endSite = s.target.Jump()
			if e := s.target.Patch(fr.falseSite, s.target.Position()); e != nil {
				return 0, e
			}
			fr.hasElse = true
			s.restore(fr.base)
		case 0x0b:
			if len(s.controls) == 0 {
				if !returned {
					id := s.pop()
					reg := s.materialize(id, 0)
					s.target.Return(reg, s.node(id).wide, s.maxSlot)
					s.release(id)
				}
				s.target = nil
				s.regs = nil
				if m := s.Memory(); m > s.Peak {
					s.Peak = m
				}
				return s.maxSlot, nil
			}
			i := len(s.controls) - 1
			fr := s.controls[i]
			if fr.isIf {
				s.canonicalize()
				site := fr.falseSite
				if fr.hasElse {
					site = fr.endSite
				}
				if e := s.target.Patch(site, s.target.Position()); e != nil {
					return 0, e
				}
			}
			s.controls = s.controls[:i]
		case 0x0f:
			id := s.pop()
			reg := s.materialize(id, 0)
			s.target.Return(reg, s.node(id).wide, s.maxSlot)
			s.release(id)
			returned = true
		default:
			operation, wide, ok := ScalarOpcode(op)
			if !ok {
				return 0, fmt.Errorf("shared scalar: admitted opcode changed: %x", op)
			}
			s.binary(operation, wide)
		}
	}
}

// FinishWorker releases shared scratch after the worker's last function.
func (s *ScalarState) FinishWorker() {
	s.Discarded += s.Memory()
	s.nodes = nil
	s.stack = nil
	s.locals = nil
	s.widths = nil
	s.controls = nil
	s.freeSlots = nil
	s.target = nil
	s.regs = nil
}
