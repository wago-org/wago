//go:build arm64

package arm64

var popcountPairEnabled = true

// Combine two scalar population counts in one byte-lane reduction. Only
// concrete, pure inputs participate: no deferred loads, traps, or calls move.
// Integer addition wraps, so one adjacent concrete accumulator may be reassociated.
func (f *fn) tryPopcountPair(node *elem, dest Reg) Reg {
	if !f.opt(optPopcountPair) || node.deferredOp() != opAdd {
		return regNone
	}
	left, right := f.s.arg0(node), f.s.arg1(node)
	var accumulator *elem
	isPop := func(e *elem) bool {
		return e != nil && e.isDeferred() && e.deferredOp() == opPopcnt && e.st.typ == node.st.typ
	}
	if !isPop(left) {
		left, right = right, left
	}
	if !isPop(left) {
		return regNone
	}
	if !isPop(right) {
		if right == nil || !right.isDeferred() || right.deferredOp() != opAdd || right.st.typ != node.st.typ {
			return regNone
		}
		a, b := f.s.arg0(right), f.s.arg1(right)
		if !isPop(a) {
			a, b = b, a
		}
		if !isPop(a) {
			return regNone
		}
		right, accumulator = a, b
	}
	pure := func(e *elem) bool {
		if e == nil || e.elemKind() != ekValue || e.st.typ != node.st.typ {
			return false
		}
		switch e.st.kind {
		case stConst, stReg, stLocalReg, stLocalRef, stGlobReg, stSlot:
			return true
		}
		return false
	}
	a, b := f.s.arg0(left), f.s.arg0(right)
	if !pure(a) || !pure(b) || (accumulator != nil && !pure(accumulator)) {
		return regNone
	}
	savedPins := f.pinned
	acc, accOwned := regNone, false
	if accumulator != nil {
		acc, accOwned = f.materializeRead(accumulator)
		f.pinned = f.pinned.add(acc)
	}
	y, yOwned := f.materializeRead(b)
	f.pinned = f.pinned.add(y)
	x, xOwned := f.materializeRead(a)
	f.pinned = f.pinned.add(x)
	v := f.allocFReg(0)
	wide := node.st.typ.is64()
	f.a.FmovFromGpr(v, x, wide)
	if wide {
		f.a.NeonInsD(v, y, 1)
	} else {
		f.a.NeonInsS(v, y, 1)
	}
	f.a.Cnt8b(v, v)
	if wide {
		f.a.NeonAddvB(v, v)
	} else {
		f.a.Addv8b(v, v)
	}
	if dest == regNone {
		dest = f.allocReg(0)
	}
	count := dest
	if accumulator != nil && dest == acc {
		count = f.allocReg(0)
	}
	f.a.FmovToGpr(count, v, false)
	if accumulator != nil {
		if wide {
			f.a.Add64(dest, count, acc)
		} else {
			f.a.Add32(dest, count, acc)
		}
	}
	if count != dest {
		f.release(count)
	}
	f.releaseF(v)
	f.pinned = savedPins
	if xOwned && x != dest {
		f.release(x)
	}
	if yOwned && y != dest {
		f.release(y)
	}
	if accOwned && acc != dest {
		f.release(acc)
	}
	f.stats.peep("popcount-pair")
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	return dest
}
