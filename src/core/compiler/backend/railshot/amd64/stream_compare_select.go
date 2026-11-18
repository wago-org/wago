//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

var streamingCompareSelectEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_COMPARE_SELECT") == "1"

// tryCompareSelect preserves a deferred integer comparison's flags through the
// select, after all potentially allocating/fixed-register work has completed.
// Pure expensive-arm selection retains priority at the caller. Rejection may
// eagerly materialize pure inputs, but does not consume any reference.
func (s *streamState) tryCompareSelect(cond, right, left streamID) bool {
	if !streamingCompareSelectEnabled {
		return false
	}
	n := *s.node(cond)
	if n.kind != streamDeferred || !streamCompare(n.op) || n.refs != 1 {
		return false
	}
	// Four distinct inputs and a disjoint output are the worst case. Do not
	// depend on aliases to make an otherwise impossible register lease succeed.
	available := 0
	for _, r := range s.regs {
		if s.reserved&(uint64(1)<<r) == 0 {
			available++
		}
	}
	if available < 5 {
		return false
	}
	// Resolve every deferred fixed-role expression before caching registers.
	// Never materialize cond itself: it is the comparison we intend to consume.
	for _, id := range []streamID{n.left, n.right, right, left} {
		if s.node(id).kind == streamDeferred {
			s.materialize(id, 0)
		}
	}
	current := *s.node(cond)
	if current.kind != streamDeferred || current.refs != 1 || current.op != n.op || current.left != n.left || current.right != n.right {
		return false
	}
	saved := s.reserved
	defer func() { s.reserved = saved }()
	lr := s.materialize(left, 0)
	s.reserved |= uint64(1) << lr
	rr := s.materialize(right, 0)
	s.reserved |= uint64(1) << rr
	cr := s.materialize(n.left, 0)
	s.reserved |= uint64(1) << cr
	rhs := s.operand(n.right)
	if !s.target.Operand(n.op, n.operandWide, rhs) {
		r := s.materialize(n.right, 0)
		s.reserved |= uint64(1) << r
	} else if rhs.Kind == shared.ScalarRegister {
		s.reserved |= uint64(1) << rhs.Reg
	}
	dst := s.alloc(0)
	// Allocation may evacuate an unreserved backing. Resolve the RHS identity
	// again; all register operands and both arms are leased until after CMOV.
	rhs = s.operand(n.right)
	if !s.target.Operand(n.op, n.operandWide, rhs) {
		return false
	}
	wide := s.node(left).wide
	s.forgetRegisterBacking(dst)
	s.target.Move(dst, rr, wide)
	s.target.scalarCmp(n.op, n.operandWide, Reg(cr), rhs)
	s.target.a.Cmovcc(scalarCondition(n.op), Reg(dst), Reg(lr), wide)
	s.reserved = saved
	s.integerResult(dst, wide, cond, right, left)
	s.target.stats.peep("stream-compare-select")
	return true
}
