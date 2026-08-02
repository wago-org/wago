//go:build amd64

package amd64

// Exact non-null GC reference facts are deliberately compact and local. A
// storage's cval is otherwise unused for reference-valued registers/local reads;
// type+1 keeps zero as "unknown". Facts never cache raw heap pointers and are
// therefore valid across collection. Structured control boundaries clear the
// local table instead of building SSA/phi state.

func exactGCType(e *elem) (uint32, bool) {
	if e == nil || e.kind != ekValue || !e.st.gcRoot || e.st.cval <= 0 {
		return 0, false
	}
	return uint32(e.st.cval - 1), true
}

func markExactGCType(e *elem, typeIndex uint32) {
	if e == nil || e.kind != ekValue {
		return
	}
	e.st.gcRoot = true
	e.st.cval = int64(typeIndex) + 1
}

func (f *fn) markTopExactGCType(typeIndex uint32) {
	if exactGCRefFactsEnabled {
		markExactGCType(f.s.back(), typeIndex)
	}
}

func (f *fn) markLocalGetExactGCType(e *elem, local int) {
	if !exactGCRefFactsEnabled || local < 0 || local >= len(f.localExactGCType) {
		return
	}
	if encoded := f.localExactGCType[local]; encoded != 0 {
		markExactGCType(e, encoded-1)
	}
}

func (f *fn) setLocalExactGCType(local int, source *elem) (uint32, bool) {
	if !exactGCRefFactsEnabled || local < 0 || local >= len(f.localExactGCType) {
		return 0, false
	}
	typeIndex, ok := exactGCType(source)
	if ok {
		f.localExactGCType[local] = typeIndex + 1
		return typeIndex, true
	}
	f.localExactGCType[local] = 0
	return 0, false
}

func (f *fn) clearLocalExactGCTypes() {
	if exactGCRefFactsEnabled {
		clear(f.localExactGCType)
	}
}

func (f *fn) topExactGCLocal() (local int, typeIndex uint32, ok bool) {
	if !exactGCRefFactsEnabled {
		return 0, 0, false
	}
	e := f.s.back()
	if e == nil || e.kind != ekValue {
		return 0, 0, false
	}
	switch e.st.kind {
	case stLocalRef, stLocalReg:
		local = e.st.idx
	default:
		return 0, 0, false
	}
	if local < 0 || local >= len(f.localExactGCType) {
		return 0, 0, false
	}
	encoded := f.localExactGCType[local]
	if encoded == 0 {
		return 0, 0, false
	}
	return local, encoded - 1, true
}

func (f *fn) refineTopLocalExactGCType(typeIndex uint32) {
	if !exactGCRefFactsEnabled {
		return
	}
	e := f.s.back()
	if e == nil || e.kind != ekValue {
		return
	}
	if e.st.kind != stLocalRef && e.st.kind != stLocalReg {
		return
	}
	local := e.st.idx
	if local >= 0 && local < len(f.localExactGCType) {
		f.localExactGCType[local] = typeIndex + 1
	}
}
