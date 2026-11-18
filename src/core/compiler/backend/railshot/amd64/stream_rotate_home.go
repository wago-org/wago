//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var streamingRotateHomeEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_ROTATE_HOME") == "1"

// A backing node owns its cell reference, so the cell cannot recycle while this
// token is live. Its fresh node order distinguishes reused arena identities.
// Only the immediately following local.set/tee may consume this tagged payload.
func streamRotateSetToken(order uint16, local int, cell uint32) int64 {
	return int64(uint64(order)<<48 | uint64(local+1)<<32 | uint64(cell))
}

// tryRotateIntoLocal only peeks: the driver still processes the local.set/tee and
// its source PC. Callers exclude lifetime tapes; all other successors retain
// ordinary lowering. No additional native operation may enter the gap.
func (s *streamState) tryRotateIntoLocal(r *wasm.Reader, op byte) bool {
	if !streamingRotateHomeEnabled || !streamingRorxEnabled || !streamingPreserveDenseEnabled || !streamingCopyProofEnabled || s.residency == nil || s.target == nil || !s.target.bmi2Rorx || r == nil || len(s.stack) < 2 {
		return false
	}
	wide, ok := streamRotateShape(op)
	if !ok {
		return false
	}
	look := *r
	next, err := look.Byte()
	if err != nil || (next != 0x21 && next != 0x22) {
		return false
	}
	x, err := look.U32()
	if err != nil || uint64(x) >= uint64(len(s.locals)) || s.widths[x] != wide {
		return false
	}
	local := int(x)
	countID, value := s.stack[len(s.stack)-1], s.stack[len(s.stack)-2]
	count, input := *s.node(countID), *s.node(value)
	if count.refs == 0 || input.refs == 0 || count.kind != shared.ScalarConstant || count.wide != wide || input.wide != wide {
		return false
	}
	old := s.locals[local]
	if old == 0 || old == value {
		return false
	}
	before := *s.node(old)
	cell := streamNodeBacking(before)
	if cell == 0 || uint64(cell) >= uint64(len(s.residency.cells)) {
		return false
	}
	// A single source edge can disappear during materialization. Other old-value
	// observers reject before allocating, rather than generating speculative spills.
	if before.refs != 1 && !(before.refs == 2 && input.kind == streamDeferred && input.refs == 1 && ((input.left == old) != (input.right == old))) {
		return false
	}
	k := -1
	for i, n := range s.homeLocals {
		if n != 0 && int(n)-1 == local && s.residency.homes[i] == cell {
			k = i
			break
		}
	}
	if k < 0 {
		return false
	}
	home := s.homeRegs[k]
	c := s.residency.cells[cell]
	if home >= 64 || c.refs != 2 || c.kind != shared.ScalarRegister || c.reg != home || c.wide != wide || s.residency.registers[home] != cell || s.owners[home] != 0 || s.reserved&(uint64(1)<<home) != 0 {
		return false
	}
	saved := s.reserved
	s.reserved |= uint64(1) << home
	defer func() { s.reserved = saved }()
	source := s.materialize(value, uint64(1)<<home)
	// Materialization may move nodes and cells. Re-read every ownership proof;
	// a failed postcheck leaves both stack references intact for ordinary lowering.
	if s.locals[local] != old || s.node(old).refs != 1 || streamNodeBacking(*s.node(old)) != cell || s.residency.homes[k] != cell {
		return false
	}
	c = s.residency.cells[cell]
	if c.refs != 2 || c.kind != shared.ScalarRegister || c.reg != home || c.wide != wide || s.residency.registers[home] != cell || s.owners[home] != 0 || source == home || s.node(value).kind != shared.ScalarRegister || s.node(value).reg != source || s.owners[source] != value {
		return false
	}
	mask := uint64(31)
	if wide {
		mask = 63
	}
	amount := byte(uint64(count.constant) & mask)
	rightAmount := amount
	if op == 0x77 || op == 0x89 {
		rightAmount = byte((-uint64(amount)) & mask)
	}
	s.pop()
	s.pop()
	// The only old observer is the overwritten local root. Retire it and the
	// map reference before reusing the physical home; no old snapshot is lost.
	s.setLocalBinding(int(local), 0)
	s.release(old)
	s.residency.homes[k] = 0
	s.releaseBacking(cell)
	if amount == 0 {
		s.target.Move(home, source, wide)
	} else {
		s.target.a.Rorx(Reg(home), Reg(source), rightAmount, wide)
		s.target.stats.peep("streaming-rotate-rorx")
		s.target.stats.peep("bmi2-rorx")
	}
	// No allocator or source release occurs between final resolution and read.
	s.release(countID)
	s.release(value)
	cell = s.newBacking(home, wide)
	s.residency.homes[k] = cell
	result := s.borrowBacking(cell, local)
	n := s.node(result)
	n.constant = streamRotateSetToken(n.order, local, cell)
	s.stack = append(s.stack, result)
	s.target.stats.peep("stream-rotate-home")
	return true
}

// commitRotateIntoLocal consumes only a producer's exact one-instruction
// handoff, not arbitrary canonical backing values. For tee, the driver retains
// the freshly bound local once after this transfer, creating its stack alias.
// Ordinary materialization
// overwrites this payload with a register proof, so it cannot revive a token.
func (s *streamState) commitRotateIntoLocal(local int, id, old streamID) bool {
	if !streamingRotateHomeEnabled || s.residency == nil || old != 0 || id == 0 || local < 0 || local >= len(s.locals) {
		return false
	}
	n := s.node(id)
	if n.kind != streamBacking || n.refs != 1 || n.slot <= 0 || n.wide != s.widths[local] || n.home != s.localHomeTag(local) {
		return false
	}
	cell := uint32(n.slot)
	if uint64(cell) >= uint64(len(s.residency.cells)) || n.constant != streamRotateSetToken(n.order, local, cell) {
		return false
	}
	for k, x := range s.homeLocals {
		if x == 0 || int(x)-1 != local || s.residency.homes[k] != cell {
			continue
		}
		c := s.residency.cells[cell]
		home := s.homeRegs[k]
		if home >= 64 || c.refs != 2 || c.kind != shared.ScalarRegister || c.reg != home || c.wide != s.widths[local] || s.residency.registers[home] != cell || s.owners[home] != 0 {
			return false
		}
		n.constant = 0
		s.setLocalBinding(int(local), id)
		s.target.stats.peep("stream-rotate-home-set")
		return true
	}
	return false
}
