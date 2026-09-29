//go:build amd64

package amd64

func (f *fn) markGCReference(e *elem) {
	if e != nil && e.isValue() {
		e.st.setGCRoot(true)
		if f.s != nil && e.st.hasLogicalRoot() {
			f.s.hasGCRoots = true
		}
	}
}

func (f *fn) markTopGCReference() { f.markGCReference(f.s.back()) }

func (f *fn) setStackGCRoot(e *elem, root bool) {
	if e == nil {
		return
	}
	e.st.setGCRoot(root)
	if root && e.isValue() && e.st.hasLogicalRoot() && f.s != nil {
		f.s.hasGCRoots = true
	}
}

// Stack values that alias mutable module state (locals/globals) are realized
// before that state is overwritten by scanning the operand stack directly
// (realizeLocalRefs in driver.go, realizeGlobalRefs in globals.go). Those scans
// are the only consumers, and they read each elem's storage, not any auxiliary
// index — so no separate occurrence map is kept. The stack is shallow, so the
// scan is cheap; a per-key map only added hashing + linked-list maintenance on
// every push/pop/replace with no reader on the other side.

func (f *fn) replaceStorage(e *elem, st storage) {
	f.s.canonicalSlots = false
	// Replacements move the same semantic value between registers, locals, and
	// spills. Preserve collector-root identity; raw resolved addresses live in
	// separate function state and are never copied here.
	st.setGCRoot(st.hasGCRoot() || e.st.hasGCRoot())
	st.setLogicalRoot(e.st.hasLogicalRoot())
	if st.hasGCRoot() && st.hasLogicalRoot() {
		f.s.hasGCRoots = true
	}
	if st.typ == mtCustom {
		st.cold = e.st.cold
	}
	e.st = st
}

func (f *fn) pushValue(st storage) *elem {
	e := f.s.pushValue(st)
	if profileEnabled && f.stats != nil && f.stats.RecordSources && st.kind == stMemRef {
		f.rememberProfileNode(e)
	}
	return e
}

func (f *fn) erase(e *elem) {
	f.s.clearElemCold(e)
	f.s.erase(e)
}
