//go:build arm64

package arm64

func (f *fn) discardTree(e *elem) {
	if e.elemKind() == ekDeferred {
		arg0, arg1 := f.s.arg0(e), f.s.arg1(e)
		if arg1 != nil {
			f.discardTree(arg1)
		}
		f.discardTree(arg0)
		f.erase(e)
		return
	}
	switch e.st.kind {
	case stReg:
		if e.st.typ == mtCustom {
			for _, reg := range f.s.elemCold(e).vregs {
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
