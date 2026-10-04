package shared

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"unsafe"
)

// ScalarSummary is admission metadata, not an instruction array. Admission is
// performed on validated bytecode before any function instruction is emitted.
type ScalarSummary struct {
	Eligible, HasIf bool
	MaxControlDepth uint8
	MaxStack, Nodes int
}

const scalarMaxNodes = 16384

// AdmitScalar admits only numeric signatures and nontrapping integer bodies.
// Indexed block signatures, loops, arbitrary branches, calls, effects, refs and
// mixed register banks stay on the established function compiler.
func AdmitScalar(code []byte, ft *wasm.CompType, localTypes []wasm.ValType) ScalarSummary {
	if len(localTypes) < len(ft.Params) {
		return ScalarSummary{}
	}
	var zeros [2]bool
	for i, t := range localTypes {
		if !scalarInteger(t) {
			return ScalarSummary{}
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

func scalarInteger(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64)
}

// AdmitScalarFunction reads the existing run-length declarations directly.
// Admission needs only the count and zero widths, not an expanded type array.
func AdmitScalarFunction(c *wasm.Func, ft *wasm.CompType) ScalarSummary {
	nLocals := len(ft.Params)
	var zeros [2]bool
	for _, run := range c.Locals.Runs {
		if run.Count == 0 {
			continue // Validated zero-count runs introduce no values or widths.
		}
		if !scalarInteger(run.Type) || run.Count > 256 || nLocals > 256-int(run.Count) {
			return ScalarSummary{}
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

func admitScalar(code []byte, ft *wasm.CompType, nLocals int, zeroWidths [2]bool) ScalarSummary {
	s := ScalarSummary{Nodes: len(ft.Params) + 1}
	// Preserve the pilot's admission budget independently of storage bounds.
	budget := nLocals + 1
	if len(code) == 0 || len(code) > 64<<10 || nLocals > 256 || len(ft.Results) != 1 {
		return s
	}
	for _, t := range ft.Params {
		if !scalarInteger(t) {
			return s
		}
	}
	if !scalarInteger(ft.Results[0]) {
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
		if budget > scalarMaxNodes {
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
			// Exclude the function frame: CompileScalar stores only explicit
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
			if _, _, ok := ScalarOpcode(op); !ok {
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
	if budget > scalarMaxNodes {
		return ScalarSummary{}
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

func scalarCommutative(op IntOp) bool {
	switch op {
	case IntAdd, IntMul, IntAnd, IntOr, IntXor, IntEq, IntNe:
		return true
	}
	return false
}

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

// Every live value has one authoritative location and a stable arena index.
// An index may be reused only after its last reference and deferred edges die.
// refs includes local owners, logical roots and deferred-parent edges.
type scalarNode struct {
	constant          int64
	left, right       scalarID
	refs, order       uint16
	slot              int32
	op                IntOp
	kind              ScalarLocation
	reg, depth        uint8
	wide, operandWide bool
	// home is a still-valid local-memory copy, encoded as local index + 1.
	// Zero means no copy; it is independent of the authoritative location.
	home uint16
}
type scalarControl struct {
	base, result  int
	isIf, hasElse bool
	// One result/local alias, encoded as local index + 1, is enough to
	// preserve a common tee across both incoming edges without a state map.
	resultLocal        uint16
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
	if len(s.nodes) != 0 {
		// The sentinel owns creation order and the free-list head. Reusing a
		// dead index never changes the age or location of a live value.
		if scalarValueChecks && int(s.nodes[0].order) >= scalarMaxNodes {
			panic("shared scalar: admitted creation bound exceeded")
		}
		s.nodes[0].order++
		n.order = s.nodes[0].order
		if id := s.nodes[0].left; id != 0 {
			if scalarValueChecks && s.node(id).refs != 0 {
				panic("shared scalar: live value in free list")
			}
			s.nodes[0].left = s.node(id).left
			s.nodes[id] = n
			return id
		}
	}
	if len(s.nodes) == cap(s.nodes) {
		capacity := max(1, 2*cap(s.nodes))
		nodes := make([]scalarNode, len(s.nodes), capacity)
		copy(nodes, s.nodes)
		s.Discarded += uint64(cap(s.nodes)) * uint64(unsafe.Sizeof(scalarNode{}))
		s.nodes = nodes
	}
	s.nodes = append(s.nodes, n)
	return scalarID(len(s.nodes) - 1)
}

// Admission bounds all references by two deferred edges per node plus at most
// 256 local bindings and 512 operand roots. That fits uint16 without widening
// scalarNode; the unchanged 16,384-node creation budget also bounds its order.
const scalarMaxReferences = 2*scalarMaxNodes + 256 + 512

func (s *ScalarState) retain(id scalarID) {
	n := s.node(id)
	if scalarValueChecks && (id == 0 || n.refs == 0 || int(n.refs) >= scalarMaxReferences) {
		panic("shared scalar: invalid retained reference")
	}
	n.refs++
}
func (s *ScalarState) release(id scalarID) {
	if id == 0 {
		return
	}
	n := s.node(id)
	if scalarValueChecks && n.refs == 0 {
		panic("shared scalar: released dead value")
	}
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
	// Deferred children are released before their fields become allocator
	// linkage. No local, root or parent may retain this index now.
	n.left = s.nodes[0].left
	s.nodes[0].left = id
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
	if n.home != 0 {
		// The unchanged local home is already a valid backing copy. Eviction
		// needs no store or temporary slot; identity and references stay intact.
		s.owners[n.reg] = 0
		n.kind = scalarBorrow
		n.slot = int32(n.home - 1)
		return
	}
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
	var victim scalarID
	var victimOrder uint16
	var reg uint8
	protected := true
	for _, r := range s.regs {
		id := s.owners[r]
		if avoid&(1<<r) != 0 || id == 0 {
			continue
		}
		n := s.node(id)
		keep := n.home != 0 || n.refs > 1
		// Creation order increases in bytecode order. Among equally reusable
		// values, evict an older value so recent stack results can begin a
		// reduction in registers. Homes and multiple references suggest reuse;
		// neither is a promise about future uses. This scans only the register
		// bank and leaves active operands and fixed registers excluded above.
		if victim == 0 || protected && !keep || protected == keep && n.order < victimOrder {
			victim, victimOrder, reg, protected = id, n.order, r, keep
		}
	}
	if victim != 0 {
		s.spill(victim)
		return reg
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
	if scalarCommutative(n.op) {
		left, right := s.node(n.left), s.node(n.right)
		// Realize the deeper tree first, leaving a spilled/borrowed leaf as
		// the target's optional memory operand. Otherwise prefer a register
		// accumulator over reloading a left-hand memory or constant value.
		if right.depth > left.depth || right.kind == ScalarRegister && left.kind != ScalarRegister && left.kind != scalarDeferred {
			n.left, n.right = n.right, n.left
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
func (s *ScalarState) canonicalize(registerResult bool) {
	capture := func(id scalarID) {
		n := s.node(id)
		if n.kind == scalarBorrow && s.locals[n.slot] != id || n.kind == ScalarFrame && int(n.slot) < s.tempBase {
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
		if s.node(id).home == uint16(i+1) {
			continue
		}
		r := s.materialize(id, 0)
		s.target.Store(s.target.LocalOffset(i), r, s.node(id).wide)
	}
	var resultReg uint8
	for _, r := range s.regs {
		if s.reserved&(1<<r) == 0 {
			resultReg = r
			break
		}
	}
	for i, id := range s.stack {
		r := s.materialize(id, 0)
		if registerResult && i == len(s.stack)-1 {
			// Both exits select the same physical register. All remaining owners
			// are released by restore; no live logical state needs this register.
			s.target.Move(resultReg, r, s.node(id).wide)
			continue
		}
		s.target.Store(s.target.SpillOffset(i+1), r, s.node(id).wide)
		if i+2 > s.maxSlot {
			s.maxSlot = i + 2
		}
	}
	s.restore(len(s.stack))
	if registerResult {
		id := s.stack[len(s.stack)-1]
		s.node(id).kind = ScalarRegister
		s.node(id).reg = resultReg
		s.owners[resultReg] = id
	}
}

// resultLocal records an identity relation, never a physical register. The
// second edge must independently prove the same relation before it survives.
func (s *ScalarState) resultLocal() uint16 {
	id := s.stack[len(s.stack)-1]
	for i, local := range s.locals {
		if local == id {
			return uint16(i + 1)
		}
	}
	return 0
}

func (s *ScalarState) restoreResultLocal(local uint16) {
	id := s.stack[len(s.stack)-1]
	s.release(s.locals[local-1])
	s.retain(id)
	s.locals[local-1] = id
	// canonicalize wrote this home on each incoming edge.
	s.node(id).home = local
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
		s.locals[i] = s.add(scalarNode{kind: scalarBorrow, slot: int32(i), home: uint16(i + 1), wide: wide, refs: 1})
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
	// The summary bounds total creation events, not simultaneous live values.
	// Start small and grow geometrically only when live references require it.
	if needed := min(summary.Nodes, 64); cap(s.nodes) < needed {
		s.Discarded += uint64(cap(s.nodes)) * uint64(unsafe.Sizeof(scalarNode{}))
		s.nodes = make([]scalarNode, 0, needed)
	}
	s.nodes = s.nodes[:0]
	s.add(scalarNode{})
	if cap(s.stack) < summary.MaxStack {
		s.stack = make([]scalarID, 0, summary.MaxStack)
	}
	if cap(s.locals) < len(localWide) {
		s.locals = make([]scalarID, 0, len(localWide))
	}
	s.stack = s.stack[:0]
	s.locals = s.locals[:0]
	s.widths = append(s.widths[:0], localWide...)
	if needed := int(summary.MaxControlDepth); cap(s.controls) < needed {
		capacity := max(needed, 2*cap(s.controls))
		capacity = min(capacity, 32)
		s.controls = make([]scalarControl, 0, capacity)
	}
	s.controls = s.controls[:0]
	s.freeSlots = s.freeSlots[:0]
	s.owners = [64]scalarID{}
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
	var zeros [2]scalarID
	for i, wide := range localWide {
		if i < nParams {
			s.locals = append(s.locals, s.add(scalarNode{kind: scalarBorrow, wide: wide, refs: 1, slot: int32(i), home: uint16(i + 1)}))
			continue
		}
		width := 0
		if wide {
			width = 1
		}
		id := zeros[width]
		if id == 0 {
			id = s.add(scalarNode{kind: ScalarConstant, wide: wide, refs: 1})
			zeros[width] = id
		} else {
			s.retain(id)
		}
		s.locals = append(s.locals, id)
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
				s.retain(id)
				s.stack = append(s.stack, id)
			} else {
				id := s.pop()
				// Release the overwritten binding before realization. Older stack reads
				// and deferred edges retain their own references, so only an unborrowed
				// source becomes available for in-place lowering at this sink.
				old := s.locals[x]
				if old != id && s.node(old).home == uint16(x+1) {
					// An older version may remain live. Stop using this home as
					// an eviction copy before a later agreement overwrites it.
					s.node(old).home = 0
				}
				s.release(old)
				s.materialize(id, 0)
				s.locals[x] = id
				if op == 0x22 {
					s.retain(id)
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
				s.canonicalize(false)
				fr.falseSite = s.condition(cond)
			}
			// Admission excludes br/br_if/br_table. A plain block therefore has
			// only fallthrough edges and needs no physical state agreement.
			s.controls = append(s.controls, fr)
		case 0x05:
			i := len(s.controls) - 1
			fr := &s.controls[i]
			if fr.result == 1 {
				fr.resultLocal = s.resultLocal()
			}
			s.canonicalize(fr.result == 1)
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
				alias := fr.resultLocal
				if alias != 0 && s.locals[alias-1] != s.stack[len(s.stack)-1] {
					alias = 0
				}
				s.canonicalize(fr.result == 1)
				if alias != 0 {
					s.restoreResultLocal(alias)
				}
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
