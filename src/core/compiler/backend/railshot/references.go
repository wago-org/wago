package amd64

// refKind identifies the occurrence group a stack value belongs to. Ref tracking
// is only needed for stack values that alias mutable module state (locals/globals)
// across deferred expression lowering. Owned registers and spill slots are private
// temporaries, so they deliberately avoid this map to keep hot push/pop paths
// allocation-free.
type refKind uint8

const (
	refLocal refKind = iota
	refReg
	refSlot
	refGlobal
)

type refKey struct {
	kind refKind
	id   int
}

func storageRefKey(st storage) (refKey, bool) {
	switch st.kind {
	case stLocalRef, stLocalReg:
		return refKey{kind: refLocal, id: st.idx}, true
	case stGlobReg:
		return refKey{kind: refGlobal, id: st.idx}, true
	default:
		return refKey{}, false
	}
}

func (f *fn) ensureRefs() {
	if f.refs == nil {
		f.refs = make(map[refKey]*elem)
	}
}

func (f *fn) addRef(e *elem) {
	if e == nil || e.kind != ekValue {
		return
	}
	k, ok := storageRefKey(e.st)
	if !ok {
		return
	}
	f.ensureRefs()
	head := f.refs[k]
	e.refPrev, e.refNext = head, nil
	if head != nil {
		head.refNext = e
	}
	f.refs[k] = e
}

func (f *fn) removeRef(e *elem) {
	if e == nil || e.kind != ekValue {
		e.refPrev, e.refNext = nil, nil
		return
	}
	k, ok := storageRefKey(e.st)
	if !ok || f.refs == nil {
		e.refPrev, e.refNext = nil, nil
		return
	}
	if e.refPrev != nil {
		e.refPrev.refNext = e.refNext
	}
	if e.refNext != nil {
		e.refNext.refPrev = e.refPrev
	}
	if f.refs[k] == e {
		f.refs[k] = e.refPrev
		if f.refs[k] == nil {
			delete(f.refs, k)
		}
	}
	e.refPrev, e.refNext = nil, nil
}

func (f *fn) replaceStorage(e *elem, st storage) {
	f.removeRef(e)
	e.st = st
	f.addRef(e)
}

func (f *fn) pushValue(st storage) *elem {
	e := f.s.pushValue(st)
	f.addRef(e)
	return e
}

func (f *fn) erase(e *elem) {
	f.removeRef(e)
	f.s.erase(e)
}

func (f *fn) refHead(k refKey) *elem {
	if f.refs == nil {
		return nil
	}
	return f.refs[k]
}

func (f *fn) rebuildRefs() {
	clear(f.refs)
	for e := f.s.head.next; e != f.s.head; e = e.next {
		e.refPrev, e.refNext = nil, nil
		f.addRef(e)
	}
}
