//go:build arm64

package arm64

import (
	"fmt"
	"os"
)

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
	// Capturing early changes temporary live ranges and the schedule ahead of
	// the old local's overwrite. Reserve its result register, then require half
	// the allocatable GP file to remain free without spills or pin eviction.
	// Higher-pressure code retains the
	// established leaf snapshots and delayed arithmetic schedule.
	needed := 1 + (len(gpAlloc)+1)/2
	free := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if f.regUser[r] == nil && !blocked.has(r) {
			free++
		}
		if !diagnosticsEnabled && free >= needed {
			break
		}
	}
	if diagnosticsEnabled && f.stats != nil {
		f.stats.peep(fmt.Sprintf("local-shift-snapshot-free-%d", free))
	}
	if free < needed {
		f.stats.peep("local-shift-snapshot-pressure")
		return nil
	}
	// Keep a short pure suffix with its captured input. Materializing
	// only the shifted binary can otherwise leave another copy at the suffix's
	// later sink, and retains a wider value than that sink needs.
suffix:
	for i := 0; i < 2; i++ {
		operand := f.s.next(root)
		parent := operand
		if parent != f.s.head && parent.isDeferred() && f.s.arg1(parent) == root {
			// A concrete left sibling precedes this captured block physically.
			// Preserve its source operand order, including SUB and shifts.
			operand = f.s.arg0(parent)
		} else {
			if operand == f.s.head || operand.elemKind() != ekValue {
				break
			}
			parent = f.s.next(operand)
			if parent == f.s.head || !parent.isDeferred() || f.s.arg0(parent) != root || f.s.arg1(parent) != operand {
				break
			}
		}
		if operand == nil || operand.elemKind() != ekValue || operand.st.typ != root.st.typ || parent.st.typ != root.st.typ {
			break
		}
		switch operand.st.kind {
		case stConst, stReg, stLocalReg, stGlobReg:
		default:
			break suffix
		}

		switch parent.deferredOp() {
		case opAdd, opSub, opMul, opAnd, opOr, opXor, opShl, opShrU, opShrS:
			root = parent
			f.stats.peep("local-shift-snapshot-consumer")
		default:
			break suffix
		}
	}

	f.condense(root, regNone)
	f.stats.peep("local-shift-snapshot")
	return root
}
