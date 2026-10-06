//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// tryDivRemPair retains both native results for an adjacent pair. At a div opcode,
// accept only local operands followed immediately by get(a), get(b), rem with
// the same width and signedness. No producer, effect or control edge is moved.
// Regional local lifetimes are bytecode-position sensitive, so leave them alone.
func (f *fn) tryDivRemPair(r *wasm.Reader, opcode byte, op wOp, typ machineType) bool {
	if len(f.intervalReg) != 0 || f.vectorRegion.enabled {
		return false
	}
	right := f.s.back()
	left := baseOfValentBlock(right).prev
	local := func(e *elem) bool {
		return e != f.s.head && e.isValue() && (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && e.st.typ == typ
	}
	if !local(left) || !local(right) {
		return false
	}
	look := *r
	for _, e := range []*elem{left, right} {
		next, err := look.Byte()
		if err != nil || next != 0x20 {
			return false
		}
		idx, err := look.U32()
		if err != nil || uint64(idx)+uint64(f.localBase) != uint64(e.st.idx) {
			return false
		}
	}
	next, err := look.Byte()
	if err != nil || next != opcode+2 {
		return false
	}

	// The existing lowering supplies the exact zero/overflow guards and owns
	// RAX for the quotient. Finish earlier trapping nodes in their original order.
	// The final divide also leaves the remainder in the now-free RDX register.
	f.pushBinOp(op, typ)
	f.materializePendingTraps()
	f.pushReg(RDX, typ)
	*r = look
	f.stats.peep("divrem-adjacent-pair")
	return true
}
