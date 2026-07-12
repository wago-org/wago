//go:build amd64

package amd64

// Memory-destination read-modify-write for self-updates of spilled integer
// locals. wasm hands the shape losslessly: `local.set $x (binop (local.get $x) y)`
// is `slot_x = slot_x OP y`. When $x is unpinned (lives in a frame slot) and y is
// a simple operand, this lowers to ONE instruction — `add [slot_x], y` — instead
// of load + op + store, and allocates no scratch register (which the pressure that
// spilled $x can least afford). Only add/sub/and/or/xor and only non-tee sets.

// isBareLocalGet reports whether e is exactly `local.get x` on an unpinned local
// (a bare stLocalRef leaf, not a deferred subtree).
func isBareLocalGet(e *elem, x int) bool {
	return e != nil && e.kind == ekValue && e.st.kind == stLocalRef && e.st.idx == x
}

// tryRMWSelfUpdate lowers a spilled-local self-update to a memory-destination RMW.
// Returns true when it fired (the deferred node and its operands are consumed).
func (f *fn) tryRMWSelfUpdate(e *elem, x int, tee bool) bool {
	if !localRMWEnabled || tee || e == nil || !e.isDeferred() {
		return false
	}
	enc, ok := aluTable[e.op]
	if !ok {
		return false // only add/sub/and/or/xor are RMW-lowerable
	}
	if _, _, pinned := f.pinReg(x); pinned {
		return false // only a slot-backed local benefits from doing the op in memory
	}
	if t := f.localType[x]; t != mtI32 && t != mtI64 {
		return false
	}
	// One operand must be a bare `local.get $x`; the other must be a simple
	// (non-deferred) value so the RMW does not have to condense a subtree wedged
	// between the operands on the stack.
	var other *elem
	switch {
	case isBareLocalGet(e.arg0, x):
		other = e.arg1
	case enc.comm && isBareLocalGet(e.arg1, x):
		other = e.arg0
	default:
		return false
	}
	if other == nil || other.kind != ekValue {
		return false
	}
	// The other operand must not reference $x: it would read slot_x, which the RMW
	// is about to overwrite. Excluding it keeps the ordering trivially correct.
	if subtreeRefsLocal(other, x) {
		return false
	}
	w := f.localType[x].is64()
	disp := f.localOff(x)
	if other.st.kind == stConst && fitsImm32(other.st.cval) {
		f.a.AluMI(enc.digit, RSP, disp, int32(other.st.cval), w)
	} else {
		r, owned := f.materializeRead(other)
		f.a.AluMR(enc.rr, RSP, disp, r, w)
		if owned {
			f.release(r)
		}
	}
	f.stats.peep("local-rmw")
	f.consumeBlockBelow(e)
	f.erase(e)
	f.locals[x].state = lsMem
	return true
}
