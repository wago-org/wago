//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func (f *fn) minimumMemoryAccess(memoryIndex uint32, offset uint64, size int) bool {
	if !f.opt(optMemoryMinimumBounds) {
		return false
	}
	mt, ok := f.memoryType(memoryIndex)
	if !ok || !shared.ProvenMinimumAccess(mt, 0, offset, size) {
		return false
	}
	maximum, _ := f.unsignedAddressMaximum(f.s.back(), mt.Limits.Addr64, 32)
	return shared.ProvenMinimumAccess(mt, maximum, offset, size)
}

// The transient expression tree needs no new facts or per-local storage. A
// fixed node budget bounds recursive work; unknown expressions use the full
// unsigned domain. Address lowering still evaluates every source operation.
// The unused budget is returned by value to avoid a recursive counter allocation.
func (f *fn) unsignedAddressMaximum(e *elem, wide bool, budget int) (uint64, int) {
	limit := uint64(^uint32(0))
	typ := mtI32
	if wide {
		limit = ^uint64(0)
		typ = mtI64
	}
	if e == nil || budget <= 0 || e.st.typ != typ {
		return limit, budget
	}
	budget--
	if e.elemKind() == ekValue {
		if e.st.kind == stConst {
			value := uint64(e.st.cval)
			if !wide {
				value = uint64(uint32(value))
			}
			return value, budget
		}
		return limit, budget
	}
	if e.elemKind() != ekDeferred {
		return limit, budget
	}
	op := e.deferredOp()
	switch op {
	case opAdd, opAnd, opOr, opXor, opMul, opShl, opShrU:
	default:
		return limit, budget
	}
	left, right := f.s.arg0(e), f.s.arg1(e)
	a, budget := f.unsignedAddressMaximum(left, wide, budget)
	b, budget := f.unsignedAddressMaximum(right, wide, budget)
	constant := uint64(0)
	known := right != nil && right.elemKind() == ekValue && right.st.kind == stConst && right.st.typ == typ
	if known {
		constant = uint64(right.st.cval)
	}
	return shared.UnsignedOperationMaximum(shared.IntOp(op), a, b, constant, known, wide), budget
}

var memoryMinimumBoundsEnabled = true
