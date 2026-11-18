//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"unsafe"
)

const streamBacking shared.ScalarLocation = 5

// One reference belongs to each backing node, regardless of node.refs, and
// one to each home-map entry. A frame cell exclusively owns its temporary slot.
type streamBackingCell struct {
	refs uint32
	slot int32
	reg  uint8
	kind shared.ScalarLocation
	wide bool
}
type streamResidency struct {
	cells     []streamBackingCell
	free      uint32
	homes     [streamMaxHomes]uint32
	registers [64]uint32
}

func (s *streamState) residencyBytes() uint64 {
	if s.residency == nil {
		return 0
	}
	return uint64(unsafe.Sizeof(streamResidency{})) + uint64(cap(s.residency.cells))*uint64(unsafe.Sizeof(streamBackingCell{}))
}
func (s *streamState) newBacking(reg uint8, wide bool) uint32 {
	r := s.residency
	var id uint32
	if r.free != 0 {
		id = r.free
		r.free = uint32(r.cells[id].slot)
	} else {
		id = uint32(len(r.cells))
		r.cells = append(r.cells, streamBackingCell{})
	}
	if streamValueChecks && (r.registers[reg] != 0 || s.owners[reg] != 0) {
		panic("stream residency: occupied backing register")
	}
	r.cells[id] = streamBackingCell{refs: 1, reg: reg, kind: shared.ScalarRegister, wide: wide}
	r.registers[reg] = id
	return id
}
func (s *streamState) retainBacking(id uint32) {
	c := &s.residency.cells[id]
	if streamValueChecks && (id == 0 || c.refs == 0) {
		panic("stream residency: retaining dead backing")
	}
	c.refs++
}
func (s *streamState) releaseBacking(id uint32) {
	if id == 0 {
		return
	}
	r := s.residency
	c := &r.cells[id]
	if streamValueChecks && c.refs == 0 {
		panic("stream residency: releasing dead backing")
	}
	c.refs--
	if c.refs != 0 {
		return
	}
	if c.kind == shared.ScalarRegister {
		r.registers[c.reg] = 0
	} else {
		s.freeSlots = append(s.freeSlots, int(c.slot))
	}
	c.slot = int32(r.free)
	r.free = id
}
func (s *streamState) borrowBacking(cell uint32, local int) streamID {
	s.retainBacking(cell)
	return s.add(streamNode{kind: streamBacking, slot: int32(cell), home: s.localHomeTag(local), wide: s.residency.cells[cell].wide, refs: 1})
}
func (s *streamState) backingOperand(id uint32) shared.ScalarOperand {
	c := s.residency.cells[id]
	if streamValueChecks && c.refs == 0 {
		panic("stream residency: reading dead backing")
	}
	o := shared.ScalarOperand{Kind: c.kind, Reg: c.reg}
	if c.kind == shared.ScalarFrame {
		o.Offset = s.target.SpillOffset(int(c.slot))
	}
	return o
}
func (s *streamState) evacuateBacking(id uint32) {
	c := &s.residency.cells[id]
	if c.kind != shared.ScalarRegister {
		return
	}
	slot := s.slot()
	s.target.Store(s.target.SpillOffset(slot), c.reg, c.wide)
	s.residency.registers[c.reg] = 0
	c.kind, c.slot = shared.ScalarFrame, int32(slot)
	s.Spills++
	s.target.stats.peep("stream-residency-evacuate")
}
func (s *streamState) commitBackingHomes() {
	// canonicalize has already completed deferred computations and captured
	// frame aliases. Reserve all destinations before touching any source value.
	r := s.residency
	saved := s.reserved
	var changed [streamMaxHomes]bool
	for k, n := range s.homeLocals {
		if n == 0 {
			continue
		}
		reg := s.homeRegs[k]
		s.reserved |= uint64(1) << reg
		if owner := s.owners[reg]; owner != 0 {
			s.spill(owner)
		}
		id := s.locals[n-1]
		cell := r.homes[k]
		unchanged := id != 0 && streamNodeBacking(*s.node(id)) == cell && cell != 0 && r.cells[cell].kind == shared.ScalarRegister && r.cells[cell].reg == reg
		changed[k] = !unchanged
		if unchanged && s.registerBacking(id) != 0 {
			s.target.stats.peep("stream-generation-home-hit")
		}
	}
	// Preserve every old destination generation before any destination writes.
	for k, yes := range changed {
		if yes && r.homes[k] != 0 {
			if r.cells[r.homes[k]].refs == 1 {
				// No value node can observe this obsolete generation. Dropping its map
				// reference needs neither a snapshot store nor a temporary spill slot.
				s.releaseBacking(r.homes[k])
				r.homes[k] = 0
				s.target.stats.peep("stream-residency-dead-generation")
			} else {
				s.evacuateBacking(r.homes[k])
			}
		}
	}
	for k, yes := range changed {
		if !yes || s.homeLocals[k] == 0 {
			continue
		}
		local := int(s.homeLocals[k]) - 1
		id := s.locals[local]
		if id == 0 {
			s.releaseBacking(r.homes[k])
			r.homes[k] = 0
			continue
		}
		o := s.operand(id)
		reg := s.homeRegs[k]
		switch o.Kind {
		case shared.ScalarConstant:
			s.target.Constant(reg, o.Constant, s.widths[local])
		case shared.ScalarFrame:
			s.target.Load(reg, o.Offset, s.widths[local])
			s.Reloads++
		case shared.ScalarRegister:
			s.target.Move(reg, o.Reg, s.widths[local])
		default:
			panic("stream residency: unprepared home source")
		}
		s.releaseBacking(r.homes[k])
		r.homes[k] = s.newBacking(reg, s.widths[local])
	}
	s.reserved = saved
}

func (s *streamState) backingHomeMask() uint64 {
	if s.residency == nil {
		return 0
	}
	var mask uint64
	for k, n := range s.homeLocals {
		if n != 0 {
			mask |= uint64(1) << s.homeRegs[k]
		}
	}
	return mask
}

// Full control-path restore has released every path-local value. Runtime
// incoming edges already established the fixed home agreement; no code emits.
func (s *streamState) restoreBackingMaps() {
	r := s.residency
	for k, cell := range r.homes {
		if cell != 0 {
			if streamValueChecks && r.cells[cell].refs != 1 {
				panic("stream residency: path restore retained alias")
			}
			s.releaseBacking(cell)
			r.homes[k] = 0
		}
	}
	for k, n := range s.homeLocals {
		if n != 0 {
			r.homes[k] = s.newBacking(s.homeRegs[k], s.widths[n-1])
		}
	}
}
func (s *streamState) borrowSelectedBacking(local int) (streamID, bool) {
	if s.residency != nil {
		for k, n := range s.homeLocals {
			if n != 0 && int(n)-1 == local {
				return s.borrowBacking(s.residency.homes[k], local), true
			}
		}
	}
	return 0, false
}

// Mutating a generation is permitted only when its map and the consumed old
// node are its sole cell references, and the old node has no surviving aliases.
func (s *streamState) tryBackingHomeSet(local int, id, old streamID) bool {
	if old == 0 {
		return false
	}
	expr, before := *s.node(id), *s.node(old)
	if expr.kind != streamDeferred || expr.refs != 1 || expr.left != old || expr.right == old || streamNodeBacking(before) == 0 || before.refs != 2 {
		return false
	}
	cell := streamNodeBacking(before)
	c := s.residency.cells[cell]
	if c.refs != 2 || c.kind != shared.ScalarRegister {
		return false
	}
	mapped := false
	for k, n := range s.homeLocals {
		if n != 0 && int(n)-1 == local && s.residency.homes[k] == cell {
			mapped = true
			break
		}
	}
	if !mapped {
		return false
	}
	switch expr.op {
	case shared.IntAdd, shared.IntSub, shared.IntAnd, shared.IntOr, shared.IntXor:
	default:
		return false
	}
	right := *s.node(expr.right)
	constant := right.kind == shared.ScalarConstant
	if !constant {
		rightCell := streamNodeBacking(right)
		if rightCell == 0 || rightCell == cell {
			return false
		}
		current := false
		for k, n := range s.homeLocals {
			if n != 0 && s.locals[n-1] == expr.right && s.residency.homes[k] == rightCell {
				current = true
				break
			}
		}
		if !current || s.residency.cells[rightCell].kind != shared.ScalarRegister {
			return false
		}
	}
	operand := s.operand(expr.right)
	if !s.target.Operand(expr.op, expr.operandWide, operand) {
		return false
	}
	// Reserve the expression node's future backing reference before destroying
	// the old node, whose final release consumes its own single cell reference.
	s.retainBacking(cell)
	s.release(old)
	s.release(expr.left)
	s.release(expr.right)
	s.target.Binary(expr.op, expr.operandWide, c.reg, c.reg, operand)
	value := s.node(id)
	value.kind, value.slot, value.home = streamBacking, int32(cell), s.localHomeTag(local)
	value.left, value.right, value.constant = 0, 0, 0
	value.reg, value.depth = 0, 0
	s.locals[local] = id
	s.target.stats.peep("stream-home-sink")
	s.target.stats.peep("stream-residency-sink")
	if constant {
		s.target.stats.peep("stream-home-sink-constant")
	} else {
		s.target.stats.peep("stream-home-sink-register")
	}
	return true
}

// Register payload: bit zero is the existing dirty-i32 carrier marker; upper
// bits hold one owned reference to an immutable backing generation, when any.
func streamRegisterProof(n streamNode) uint32 {
	if n.kind != shared.ScalarRegister {
		return 0
	}
	return uint32(uint64(n.constant) >> 1)
}
func (s *streamState) registerBacking(id streamID) uint32 { return streamRegisterProof(*s.node(id)) }
func streamNodeBacking(n streamNode) uint32 {
	if n.kind == streamBacking {
		return uint32(n.slot)
	}
	return streamRegisterProof(n)
}

// Called before a destination register's first destructive write. Physical
// aliases remain protected by the ordinary owner/refcount rules; only the
// immutable equality proof is relinquished here.
func (s *streamState) forgetRegisterBacking(reg uint8) {
	id := s.owners[reg]
	if id == 0 {
		return
	}
	n := s.node(id)
	if cell := streamRegisterProof(*n); cell != 0 {
		n.constant &= 1
		s.releaseBacking(cell)
	}
}
