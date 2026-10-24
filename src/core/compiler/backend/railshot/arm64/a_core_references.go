//go:build arm64

package arm64

func (f *fn) replaceStorage(e *elem, st storage) {
	// Replacements move the same semantic value between registers, locals, and
	// spills. Preserve collector-root identity and semantic provenance unless a
	// producer explicitly marks a newly-created value.
	st.setGCRoot(st.hasGCRoot() || e.st.hasGCRoot())
	st.setValueFacts(st.valueFacts() | e.st.valueFacts())
	if st.typ == mtCustom {
		st.cold = e.st.cold
	}
	e.st = st
	if regallocCheckEnabled {
		f.checkTrackMemRef(e)
	}
}

func (f *fn) pushValue(st storage) *elem {
	e := f.s.pushValue(st)
	if regallocCheckEnabled {
		f.checkTrackMemRef(e)
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources && st.kind == stMemRef {
		f.rememberProfileNode(e)
	}
	return e
}

func (f *fn) erase(e *elem) {
	if f.reuseOperands {
		f.s.detached = append(f.s.detached, f.s.prev(e).next)
	}
	f.unlinkOperand(e)
}

// unlinkOperand leaves retirement to its caller. Fully consumed expression
// children can be recycled immediately; roots remain live for this instruction.
func (f *fn) unlinkOperand(e *elem) {
	if regallocCheckEnabled {
		f.checkConsumeMemRef(e)
	}
	f.s.clearElemCold(e)
	f.s.erase(e)
}
