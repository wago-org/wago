//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"os"
)

var streamingHomeSinkEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_HOME_SINK") == "1"

// tryHomeSet consumes the local root and the two expression edges, but retains
// the expression identity as the new borrowed home. No other reference may
// observe the old destination value. Persistent homes never enter owners[].
func (s *streamState) tryHomeSet(local int, id, old streamID) bool {
	if !streamingHomeSinkEnabled {
		return false
	}
	home, ok := s.localHomeReg(local)
	if !ok {
		return false
	}
	expr, before := *s.node(id), *s.node(old)
	if expr.kind != streamDeferred || expr.refs != 1 || expr.left != old || expr.right == old || before.kind != streamBorrow || before.refs != 2 || before.slot != int32(local) || before.home != s.localHomeTag(local) {
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
		if right.kind != streamBorrow || right.slot < 0 || int(right.slot) >= len(s.locals) || s.locals[right.slot] != expr.right || right.home != s.localHomeTag(int(right.slot)) {
			return false
		}
		other, selected := s.localHomeReg(int(right.slot))
		if !selected || other == home {
			return false
		}
	}
	operand := s.operand(expr.right)
	if !s.target.Operand(expr.op, expr.operandWide, operand) {
		return false
	}
	s.target.Binary(expr.op, expr.operandWide, home, home, operand)
	s.release(old)
	s.release(expr.left)
	s.release(expr.right)
	value := s.node(id)
	value.kind, value.slot, value.home = streamBorrow, int32(local), s.localHomeTag(local)
	value.left, value.right, value.constant = 0, 0, 0
	value.reg, value.depth = 0, 0
	s.locals[local] = id
	s.target.stats.peep("stream-home-sink")
	if constant {
		s.target.stats.peep("stream-home-sink-constant")
	} else {
		s.target.stats.peep("stream-home-sink-register")
	}
	return true
}
