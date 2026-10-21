//go:build arm64

package arm64

// Keep the values linked while their loads and the condition are evaluated.
// Both branches execute in Wasm order; FCSEL only chooses the already computed
// bits, including NaN payloads and signed zero.
func (f *fn) tryScalarFloatSelect(cond *elem) bool {
	b := f.s.prev(f.s.baseOfValentBlock(cond))
	if b == f.s.head {
		return false
	}
	a := f.s.prev(f.s.baseOfValentBlock(b))
	if a == f.s.head {
		return false
	}
	typ := rootMachineType(a)
	if !typ.isFloat() || rootMachineType(b) != typ || !isFusableCondition(cond) {
		return false
	}
	old := f.fpinned
	xa, xaOwned := f.operandRegF(a)
	f.fpinned = old.add(xa)
	xb, xbOwned := f.operandRegF(b)
	f.fpinned = f.fpinned.add(xb)
	dest := xa
	if !xaOwned {
		if xbOwned {
			dest = xb
		} else {
			dest = f.allocFReg(maskOf(xa, xb))
		}
	}
	// A fresh destination has no owner yet. GP allocation during condition
	// lowering can realize a pending FP load, so pin it with the sources.
	f.fpinned = f.fpinned.add(dest)
	cc := f.condenseToFlags(cond)
	f.stats.peep("scalar-fcsel-flags")
	f.a.Fcsel(dest, xa, xb, cc, typ == mtF64)
	f.fpinned = old
	if xaOwned && xa != dest {
		f.releaseF(xa)
	}
	if xbOwned && xb != dest && xb != xa {
		f.releaseF(xb)
	}
	f.erase(b)
	f.erase(a)
	f.pushFReg(dest, typ)
	f.stats.peep("scalar-fcsel")
	return true
}
