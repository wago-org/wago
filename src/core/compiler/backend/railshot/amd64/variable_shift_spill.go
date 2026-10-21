//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// Do not change ordinary cheap CL shifts or shared scalar allocation. Admit
// only concrete operands where the fixed count would spill an unrelated value.
func (f *fn) shiftXAvoidsLiveCountSpill(node *elem, dest Reg) bool {
	if f.sc == nil || !f.sc.amd64Features.Has(shared.AMD64BMI2) || dest == RCX {
		return false
	}
	left, right := node.arg0, node.arg1
	// Called after the original lowering evaluates both operands. Keep its
	// evaluation and allocation order; change only the fixed-count spill.
	if left == nil || right == nil || !left.isValue() || !right.isValue() ||
		left.st.kind != stReg || right.st.kind != stReg || left.st.reg == RCX || right.st.reg == RCX {
		return false
	}
	live := f.regUser[RCX]
	return live != nil && live.isValue() && live.st.kind == stReg
}
