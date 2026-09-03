//go:build amd64

package amd64

// Stack values that alias mutable module state (locals/globals) are realized
// before that state is overwritten by scanning the operand stack directly
// (realizeLocalRefs in driver.go, realizeGlobalRefs in globals.go). Those scans
// are the only consumers, and they read each elem's storage, not any auxiliary
// index — so no separate occurrence map is kept. The stack is shallow, so the
// scan is cheap; a per-key map only added hashing + linked-list maintenance on
// every push/pop/replace with no reader on the other side.

func (f *fn) replaceStorage(e *elem, st storage) {
	// Replacements move the same semantic value between registers, locals, and
	// spills. Preserve collector-root identity and structured semantic facts;
	// raw resolved addresses live in separate fn state and are never copied here.
	fact := f.gcRefFact(e)
	st.gcRoot = st.gcRoot || e.st.gcRoot
	putGCRefFact(&st, fact)
	if st.typ == mtCustom {
		st.cold = e.st.cold
	}
	e.st = st
}

func (f *fn) pushValue(st storage) *elem {
	return f.s.pushValue(st)
}

func (f *fn) erase(e *elem) {
	f.s.clearElemCold(e)
	f.s.erase(e)
}
