//go:build amd64 && !tinygo && !wago_profile

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// A capture-time proof, consumed immediately after canonicalization. These
// selected homes already contain the current locals, so the agreement preserves
// their values. Detached or overwritten local identities never obtain a proof.
type streamBranchHome struct {
	constant           int64
	left, right, flags uint8 // bit0 compare; bit1 register RHS; bit2 boolean home
}

func (s *streamState) currentBorrowHome(id streamID) (uint8, bool) {
	n := s.node(id)
	if n.kind != streamBorrow || n.slot < 0 || int(n.slot) >= len(s.locals) || s.locals[n.slot] != id || n.home != s.localHomeTag(int(n.slot)) {
		return 0, false
	}
	return s.localHomeReg(int(n.slot))
}
func (s *streamState) streamCaptureCondition(id streamID) streamBranchHome {
	n := *s.node(id)
	if n.kind == streamDeferred && streamCompare(n.op) {
		if streamingHomeSinkEnabled {
			if left, ok := s.currentBorrowHome(n.left); ok {
				r := s.operand(n.right)
				if r.Kind == shared.ScalarConstant && s.target.Operand(n.op, n.operandWide, r) {
					return streamBranchHome{constant: r.Constant, left: left, flags: 1}
				}
				if right, ok := s.currentBorrowHome(n.right); ok {
					return streamBranchHome{left: left, right: right, flags: 3}
				}
			}
		}
		if !streamingHomeSinkEnabled || s.node(n.left).kind != shared.ScalarConstant {
			s.materialize(n.left, 0)
		}
		if !streamingHomeSinkEnabled || s.node(n.right).kind != shared.ScalarConstant {
			s.materialize(n.right, 0)
		}
	} else {
		if streamingHomeSinkEnabled {
			if home, ok := s.currentBorrowHome(id); ok {
				return streamBranchHome{left: home, flags: 4}
			}
		}
		s.materialize(id, 0)
	}
	return streamBranchHome{}
}
func (s *streamState) condition(id streamID, homes streamBranchHome, taken bool) int {
	n := *s.node(id)
	if n.kind == streamDeferred && streamCompare(n.op) {
		var left uint8
		var right shared.ScalarOperand
		if homes.flags&1 != 0 {
			left = homes.left
			right = shared.ScalarOperand{Kind: shared.ScalarConstant, Constant: homes.constant}
			if homes.flags&2 != 0 {
				right = shared.ScalarOperand{Kind: shared.ScalarRegister, Reg: homes.right}
			}
			s.target.stats.peep("stream-home-branch")
		} else {
			left, right = s.operands(n, 0)
		}
		s.target.scalarCmp(n.op, n.operandWide, Reg(left), right)
		cc := scalarCondition(n.op)
		if !taken {
			cc ^= 1
		}
		site := s.target.a.JccPlaceholder(cc)
		s.release(id)
		return site
	}
	reg := homes.left
	if homes.flags&4 == 0 {
		reg = s.materialize(id, 0)
	} else {
		s.target.stats.peep("stream-home-branch")
	}
	s.target.a.TestSelf(Reg(reg), false)
	cc := condE
	if taken {
		cc = condNE
	}
	site := s.target.a.JccPlaceholder(cc)
	s.release(id)
	return site
}
