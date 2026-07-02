//go:build linux && amd64

package x64

import "testing"

func TestReferencesTrackPushReplaceErase(t *testing.T) {
	f := &fn{s: newStack()}
	a := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 2})
	b := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 2})
	if got := f.refHead(refKey{kind: refLocal, id: 2}); got != b {
		t.Fatalf("local head = %p, want newest %p", got, b)
	}
	if b.refPrev != a || a.refNext != b {
		t.Fatalf("local chain not linked: b.prev=%p a.next=%p", b.refPrev, a.refNext)
	}

	f.replaceStorage(b, storage{kind: stReg, typ: mtI32, reg: R9})
	if got := f.refHead(refKey{kind: refLocal, id: 2}); got != a {
		t.Fatalf("local head after replace = %p, want %p", got, a)
	}
	if got := f.refHead(refKey{kind: refReg, id: int(R9)}); got != b {
		t.Fatalf("reg head after replace = %p, want %p", got, b)
	}

	f.erase(a)
	if got := f.refHead(refKey{kind: refLocal, id: 2}); got != nil {
		t.Fatalf("local head after erase = %p, want nil", got)
	}
	if got := f.refHead(refKey{kind: refReg, id: int(R9)}); got != b {
		t.Fatalf("reg head after unrelated erase = %p, want %p", got, b)
	}
}

func TestReferencesRebuildAndSetDepth(t *testing.T) {
	f := &fn{s: newStack()}
	f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 1})
	f.refs = nil
	f.rebuildRefs()
	if got := f.refHead(refKey{kind: refSlot, id: 1}); got == nil || got.st.slot != 1 {
		t.Fatalf("rebuilt slot head = %#v", got)
	}

	f.setDepth(1)
	if got := f.refHead(refKey{kind: refSlot, id: 1}); got != nil {
		t.Fatalf("stale slot head after setDepth = %p", got)
	}
	if got := f.refHead(refKey{kind: refSlot, id: 0}); got == nil || got.st.slot != 0 {
		t.Fatalf("slot 0 after setDepth = %#v", got)
	}
}
