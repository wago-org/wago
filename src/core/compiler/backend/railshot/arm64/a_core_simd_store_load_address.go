//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Match only an identical pure expression over unchanged locals. No operation
// between the store and load can observe memory, write locals, call or trap.
// The original tree is evaluated normally once; the actual load is retained.
func (f *fn) inspectSIMDStoreLoadAddress(source *wasm.Reader, memoryIndex uint32, off uint64) (simdStoreLoadAddress, bool) {
	var p simdStoreLoadAddress
	if !f.opt(optSIMDStoreLoadAddress) || f.guardMode || len(f.customInstructions) != 0 {
		return p, false
	}
	r := *source
	start := r.Offset()
	budget := 32
	if !f.matchRepeatedAddress(&r, f.s.back(), f.memoryAddr64(memoryIndex), &budget) || r.Offset()-start > 256 {
		return p, false
	}
	p.LoadPC = r.Offset()
	op, err := r.Byte()
	if err != nil || op != 0xfd {
		return p, false
	}
	sub, err := r.U32()
	if err != nil || sub != 0 {
		return p, false
	}
	index, offset, err := f.readMemArg(&r)
	if err != nil || index != memoryIndex || offset != off {
		return p, false
	}
	p.End = r.Offset()
	if f.opt(optSIMDLoadExtract) {
		if extract, ok := shared.InspectSIMDLoadExtract(&r); ok {
			p.Narrow = true
			p.Extract = extract
			p.End = extract.End
		}
	}
	return p, true
}

func (f *fn) matchRepeatedAddress(r *wasm.Reader, e *elem, wide bool, budget *int) bool {
	typ := mtI32
	if wide {
		typ = mtI64
	}
	if e == nil || *budget <= 0 || e.st.typ != typ {
		return false
	}
	*budget--
	if e.elemKind() == ekValue {
		switch e.st.kind {
		case stLocalRef, stLocalReg:
			op, err := r.Byte()
			if err != nil || op != 0x20 {
				return false
			}
			local, err := r.U32()
			return err == nil && uint64(local)+uint64(f.localBase) == uint64(e.st.idx)
		case stConst:
			op, err := r.Byte()
			if err != nil {
				return false
			}
			if wide {
				if op != 0x42 {
					return false
				}
				v, err := r.I64()
				return err == nil && v == e.st.cval
			}
			if op != 0x41 {
				return false
			}
			v, err := r.I32()
			return err == nil && uint32(v) == uint32(e.st.cval)
		}
		return false
	}
	if e.elemKind() != ekDeferred {
		return false
	}
	switch e.deferredOp() {
	case opAdd, opSub, opMul, opAnd, opOr, opXor, opShl, opShrU, opShrS:
	default:
		return false
	}
	if !f.matchRepeatedAddress(r, f.s.arg0(e), wide, budget) || !f.matchRepeatedAddress(r, f.s.arg1(e), wide, budget) {
		return false
	}
	op, err := r.Byte()
	if err != nil {
		return false
	}
	observed, width, ok := shared.ScalarOpcode(op)
	return ok && width == wide && observed == shared.IntOp(e.deferredOp())
}
