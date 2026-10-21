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
	xa := f.materializeF(a)
	f.fpinned = old.add(xa)
	xb := f.materializeF(b)
	f.fpinned = f.fpinned.add(xb)
	cc := f.condenseToFlags(cond)
	f.stats.peep("scalar-fcsel-flags")
	f.a.Fcsel(xa, xa, xb, cc, typ == mtF64)
	f.fpinned = old
	f.releaseF(xb)
	f.erase(b)
	f.erase(a)
	f.pushFReg(xa, typ)
	f.stats.peep("scalar-fcsel")
	return true
}
