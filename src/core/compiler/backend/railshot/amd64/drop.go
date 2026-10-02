//go:build amd64

package amd64

// dropValue removes the top value while preserving every observable Wasm side
// effect. A non-trapping deferred expression has produced no machine code yet,
// so discard its complete value tree instead of condensing a result that is
// immediately dead. Trapping integer div/rem and guard-backed deferred loads
// still materialize through the ordinary path in original evaluation order.
func (f *fn) dropValue() {
	e := f.s.back()
	if e.isDeferred() && f.treeDiscardable(e) {
		f.discardTree(e)
		f.stats.peep("pure-drop")
		return
	}
	e = f.popValue()
	f.releaseDroppedValue(e)
}

func (f *fn) treeDiscardable(e *elem) bool {
	if e == nil || e.kind == ekSkip || e.kind == ekBlock {
		return false
	}
	if e.kind == ekDeferred {
		return deferredOpDiscardable(e.op) && f.treeDiscardable(e.arg0) &&
			(e.arg1 == nil || f.treeDiscardable(e.arg1))
	}
	if e.st.ehRoot {
		return false
	}
	// In guard mode the deferred load itself is the bounds trap. Explicit mode
	// has already emitted the check, so an unconsumed load is side-effect free.
	return e.st.kind != stMemRef || !f.guardMode
}

// deferredOpDiscardable is deliberately a whitelist: adding a new deferred op
// cannot silently erase a newly introduced trap or side effect.
func deferredOpDiscardable(op wOp) bool {
	return isBinALU(op) || isShift(op) || isCompare(op) || isUnary(op) ||
		isConvert(op) || op == opEqz
}

func (f *fn) discardTree(e *elem) {
	if e.kind == ekDeferred {
		if e.arg1 != nil {
			f.discardTree(e.arg1)
		}
		f.discardTree(e.arg0)
		f.erase(e)
		return
	}
	switch e.st.kind {
	case stReg:
		if e.st.typ == mtCustom {
			for _, reg := range e.st.vregs {
				f.releaseF(reg)
			}
		} else if e.st.typ.isXMM() {
			f.releaseF(e.st.reg)
		} else {
			f.release(e.st.reg)
		}
	case stMemRef:
		f.releaseMemRef(e.st)
	}
	f.erase(e)
}

func (f *fn) releaseDroppedValue(e *elem) {
	if e.st.ehRoot {
		root, owned := f.materializeRead(e)
		zero := f.allocReg(maskOf(root))
		f.a.XorSelf32(zero)
		for off := int32(0); off < ehRootSlots*8; off += 8 {
			f.a.Store64(root, off, zero)
		}
		f.release(zero)
		if owned {
			f.release(root)
		}
		f.stats.peep("eh-root-clear")
		return
	}
	switch e.st.kind {
	case stReg:
		if e.st.typ == mtCustom {
			for _, reg := range e.st.vregs {
				f.releaseF(reg)
			}
		} else if e.st.typ.isXMM() {
			f.releaseF(e.st.reg)
		} else {
			f.release(e.st.reg)
		}
	case stMemRef:
		// In guard-page mode the load itself is the OOB trap, so a dropped load
		// must still be emitted; with explicit checks the bounds check already ran.
		if f.guardMode {
			if e.st.typ.isFloat() {
				x := f.allocFReg(0)
				f.loadFMemRef(x, e.st)
				f.releaseF(x)
			} else {
				r := f.memRefValue(e.st) // never write a borrowed address register
				f.release(r)
			}
		}
		f.releaseMemRef(e.st)
	}
}
