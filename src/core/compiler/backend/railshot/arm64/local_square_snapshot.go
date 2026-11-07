//go:build arm64

package arm64

import "os"

var localSquareSnapshotEnabled = os.Getenv("WAGO_ARM64_NO_LOCAL_SQUARE_SNAPSHOT") != "1"

// Capture a pure pending square before its borrowed local is overwritten. This
// replaces two independent old-local snapshots with one owned multiplication
// result; the result remains an ordinary spillable operand-stack node.
func (f *fn) snapshotLocalSquare(first *elem, local int) *elem {
	if !localSquareSnapshotEnabled || f.makesCalls || f.moduleEH || len(f.customInstructions) != 0 ||
		first == f.s.head || first.elemKind() != ekValue || first.st.kind != stLocalReg || first.st.idx != uint32(local) ||
		(first.st.typ != mtI32 && first.st.typ != mtI64) {
		return nil
	}
	second := f.s.next(first)
	if second == f.s.head || second.elemKind() != ekValue || second.st.kind != stLocalReg || second.st.idx != uint32(local) || second.st.reg != first.st.reg || second.st.typ != first.st.typ {
		return nil
	}
	root := f.s.next(second)
	if root == f.s.head || !root.isDeferred() || root.deferredOp() != opMul || f.s.arg0(root) != first || f.s.arg1(root) != second || root.st.typ != first.st.typ {
		return nil
	}
	input, owned := f.materializeRead(first)
	pins := f.pinned
	f.pinned = f.pinned.add(input)
	result := f.allocReg(maskOf(input))
	f.aluRR3(opMul, result, input, input, root.st.typ == mtI64)
	f.pinned = pins
	if owned {
		f.release(input)
	}
	f.consumeBlockBelow(root)
	f.occupy(root, result)
	f.stats.peep("local-square-snapshot")
	return root
}
