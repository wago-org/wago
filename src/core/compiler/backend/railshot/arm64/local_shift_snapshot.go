//go:build arm64

package arm64

import "os"

var localShiftSnapshotEnabled = os.Getenv("WAGO_ARM64_NO_LOCAL_SHIFT_SNAPSHOT") != "1"

// Capture x op (x shift constant), in either operand order, before x is
// overwritten. The initial five records are pure integer operations on the same old
// local; no load, trap, or other pending source effect moves with this cover.
// The existing shifted-register selector chooses and emits the native form.
func (f *fn) snapshotLocalShiftedBinary(first *elem, local int) *elem {
	if !localShiftSnapshotEnabled || f.makesCalls || f.moduleEH || len(f.customInstructions) != 0 ||
		first == f.s.head || first.elemKind() != ekValue || first.st.kind != stLocalReg || first.st.idx != uint32(local) ||
		(first.st.typ != mtI32 && first.st.typ != mtI64) {
		return nil
	}
	second := f.s.next(first)
	constant := f.s.next(second)
	shift := f.s.next(constant)
	root := f.s.next(shift)
	plain, shifted := first, second
	shiftFirst := second != f.s.head && second.elemKind() == ekValue && second.st.kind == stConst
	if shiftFirst {
		constant = second
		shift = f.s.next(constant)
		second = f.s.next(shift)
		root = f.s.next(second)
		plain, shifted = second, first
	}
	if second == f.s.head || second.elemKind() != ekValue || second.st.kind != stLocalReg || second.st.idx != uint32(local) || second.st.reg != first.st.reg || second.st.typ != first.st.typ {
		return nil
	}
	if constant == f.s.head || constant.elemKind() != ekValue || constant.st.kind != stConst || constant.st.typ != first.st.typ {
		return nil
	}
	if shift == f.s.head || !shift.isDeferred() || shift.st.typ != first.st.typ || f.s.arg0(shift) != shifted || f.s.arg1(shift) != constant {
		return nil
	}
	switch shift.deferredOp() {
	case opShl, opShrU, opShrS:
	default:
		return nil
	}
	if root == f.s.head || !root.isDeferred() || root.st.typ != first.st.typ {
		return nil
	}
	left, right := plain, shift
	if shiftFirst {
		left, right = shift, plain
	}
	if f.s.arg0(root) != left || f.s.arg1(root) != right {
		return nil
	}

	switch root.deferredOp() {
	case opAdd, opSub, opAnd, opOr, opXor:
	default:
		return nil
	}
	// Keep a short pure constant suffix with its captured input. Materializing
	// only the shifted binary can otherwise leave another copy at the suffix's
	// later sink, and retains a wider value than that sink needs.
suffix:
	for i := 0; i < 2; i++ {
		c := f.s.next(root)
		if c == f.s.head || c.elemKind() != ekValue || c.st.kind != stConst || c.st.typ != root.st.typ {
			break
		}
		parent := f.s.next(c)
		if parent == f.s.head || !parent.isDeferred() || parent.st.typ != root.st.typ || f.s.arg0(parent) != root || f.s.arg1(parent) != c {
			break
		}
		switch parent.deferredOp() {
		case opAdd, opSub, opMul, opAnd, opOr, opXor, opShl, opShrU, opShrS:
			root = parent
		default:
			break suffix
		}
	}
	f.condense(root, regNone)
	f.stats.peep("local-shift-snapshot")
	return root
}
