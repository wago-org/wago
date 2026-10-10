//go:build arm64

package arm64

import "os"

var selectSourceReadEnabled = os.Getenv("WAGO_ARM64_NO_SELECT_SOURCE_READ") != "1"

func (f *fn) selectSinkRead(e *elem) (Reg, bool) {
	if f.opt(optSelectSourceRead) {
		if e.elemKind() == ekValue && e.st.kind == stConst {
			return f.intConstReadReg(e.st, f.pinned)
		}
		return f.materializeRead(e)
	}
	return f.materialize(e), true
}

// Temporarily number a protected select constant so its comparison need not
// materialize the same value again. The caller restores the old cache extent.
func (f *fn) selectSinkPublishConst(st storage, r Reg) {
	if st.kind != stConst || (st.typ != mtI32 && st.typ != mtI64) || int(f.iconstN) >= len(f.iconsts) {
		return
	}
	v := st.cval
	if st.typ == mtI32 {
		v = int64(int32(v))
	}
	if f.fitsAddSubImmediate(v) {
		return
	}
	if _, found := f.cachedIntConst(st); found {
		return
	}
	f.iconsts[f.iconstN] = intConstReg{typ: st.typ, bits: st.cval, reg: r}
	f.iconstN++
	f.stats.peep("select-const-reuse")
}
