//go:build amd64

package amd64

// detachControlIntervalAddresses preserves a deferred load's address when its
// regional local lease must end. Executing that load while realizing references
// to a local can overtake an older trapping expression on the operand stack.
func (f *fn) detachControlIntervalAddresses() regMask {
	var borrowed *elem
	loads := 0
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if !e.isValue() || e.st.kind != stMemRef {
			continue
		}
		loads++
		if x := e.st.memBorrow(); x >= 0 && f.intervalOwner[e.st.reg] == x {
			borrowed = e
		}
	}
	if borrowed == nil {
		return 0
	}
	// With multiple pending loads, allocating a new carrier could force a later
	// load to free a register. Reconcile the stack in source order instead.
	if loads != 1 {
		f.stats.peep("interval-address-flush")
		f.flush()
		return 0
	}
	src := borrowed.st.reg
	// Fixed-role operations can spill their mandatory registers regardless of
	// reservations. Keep the carrier away from divide and shift operands.
	dst := f.allocRegOrNone(maskOf(src, RAX, RDX, RCX))
	if dst == regNone {
		f.stats.peep("interval-address-flush")
		f.flush()
		return 0
	}
	f.moveInt(dst, src, mtI64)
	borrowed.st.reg = dst
	borrowed.st.cval = 0 // memBorrow() == -1: the load now owns its carrier.
	f.regUser[dst] = borrowed
	f.stats.peep("interval-address-detach")
	return maskOf(dst)
}
