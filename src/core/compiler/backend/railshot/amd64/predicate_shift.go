//go:build amd64

package amd64

import "os"

var predicateShiftEnabled = os.Getenv("WAGO_AMD64_PREDICATE_SHIFT") != "0"

// An integer comparison produces exactly 0 or 1. Select between the original
// value and its one-bit shift using the comparison flags, without SETcc or CL.
// Keep both candidates outside fixed registers until the predicate is consumed:
// its subtrees can contain division or another variable shift.
func (f *fn) tryPredicateShift(node *elem, dest Reg) (Reg, bool) {
	right := node.arg1
	if !predicateShiftEnabled || node.valueType() != mtI32 || !isFusableCompare(right) || right.valueType().isFloat() {
		return regNone, false
	}
	value := f.materializeSelectBranch(node.arg0, mtI32)
	f.pinned = f.pinned.add(value)
	shifted := f.allocReg(maskOf(RAX, RDX, RCX, value))
	f.pinned = f.pinned.add(shifted)
	f.a.MovRegReg32(shifted, value)
	f.a.ShiftImm(shiftDigit(node.deferredOp()), shifted, 1, false)
	cc := f.condenseToFlags(right)
	f.a.Cmovcc(cc, value, shifted, false)
	f.pinned = f.pinned.remove(shifted).remove(value)
	f.release(shifted)
	result := value
	if dest != regNone && dest != value {
		f.moveInt(dest, value, mtI32)
		f.release(value)
		result = dest
	}
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	node.setDeferredOp(opNone)
	f.stats.peep("predicate-shift")
	return result, true
}
