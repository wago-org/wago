//go:build arm64

package arm64

import "os"

var ownedResultSinkEnabled = os.Getenv("WAGO_ARM64_NO_OWNED_RESULT_SINK") != "1"

// The ordinary RHS realization has already completed before this cover runs.
// Both remaining inputs are concrete registers, so the ALU can read them before
// writing a distinct requested result without first copying the owned left.
func (f *fn) tryOwnedResultSink(node, left, right *elem, dest Reg) Reg {
	if node.deferredOp() != opXor || !ownedResultSinkEnabled || !f.opt(optThreeOpSink) || dest == regNone ||
		left.elemKind() != ekValue || left.st.kind != stReg || left.st.reg == dest ||
		right.elemKind() != ekValue {
		return regNone
	}
	switch right.st.kind {
	case stReg, stLocalReg, stGlobReg:
	default:
		return regNone
	}
	f.aluRR3(node.deferredOp(), dest, left.st.reg, right.st.reg, node.st.typ.is64())
	f.release(left.st.reg)
	if right.st.kind == stReg && right.st.reg != dest {
		f.release(right.st.reg)
	}
	f.stats.peep("owned-result-sink")
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	return dest
}
