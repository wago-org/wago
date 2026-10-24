//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

var memoryMinimumBoundsEnabled = true

func (f *fn) minimumMemoryAccess(memoryIndex uint32, offset uint64, size int) bool {
	if !f.opt(optMemoryMinimumBounds) {
		return false
	}
	mt, ok := f.memoryType(memoryIndex)
	if !ok || !shared.ProvenMinimumAccess(mt, 0, offset, size) {
		return false
	}
	budget := 32
	maximum := f.unsignedAddressMaximum(f.s.back(), mt.Limits.Addr64, &budget)
	return shared.ProvenMinimumAccess(mt, maximum, offset, size)
}

// The transient expression tree needs no new facts or per-local storage. A
// fixed node budget bounds recursive work; unknown expressions use the full
// unsigned domain. Address lowering still evaluates every source operation.
func (f *fn) unsignedAddressMaximum(e *elem, wide bool, budget *int) uint64 {
	limit := uint64(^uint32(0))
	typ := mtI32
	if wide {
		limit = ^uint64(0)
		typ = mtI64
	}
	if e == nil || *budget <= 0 || e.st.typ != typ {
		return limit
	}
	*budget--
	if e.elemKind() == ekValue {
		if e.st.kind == stConst {
			value := uint64(e.st.cval)
			if !wide {
				value = uint64(uint32(value))
			}
			return value
		}
		return limit
	}
	if e.elemKind() != ekDeferred {
		return limit
	}
	op := e.deferredOp()
	switch op {
	case opAdd, opAnd, opOr, opXor, opMul, opShl, opShrU:
	default:
		return limit
	}
	left, right := f.s.arg0(e), f.s.arg1(e)
	a := f.unsignedAddressMaximum(left, wide, budget)
	b := f.unsignedAddressMaximum(right, wide, budget)
	constant := uint64(0)
	known := right != nil && right.elemKind() == ekValue && right.st.kind == stConst && right.st.typ == typ
	if known {
		constant = uint64(right.st.cval)
	}
	return shared.UnsignedOperationMaximum(shared.IntOp(op), a, b, constant, known, wide)
}
