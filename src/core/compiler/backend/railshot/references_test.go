//go:build linux && amd64

package amd64

import "testing"

func TestReferencesTrackLocalPushReplaceErase(t *testing.T) {
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
	if got := f.refHead(refKey{kind: refReg, id: int(R9)}); got != nil {
		t.Fatalf("owned reg should not be tracked, got %p", got)
	}
	if b.refPrev != nil || b.refNext != nil {
		t.Fatalf("untracked replacement kept ref links: prev=%p next=%p", b.refPrev, b.refNext)
	}

	f.erase(a)
	if got := f.refHead(refKey{kind: refLocal, id: 2}); got != nil {
		t.Fatalf("local head after erase = %p, want nil", got)
	}
}

func TestReferencesSkipOwnedRegsAndSlots(t *testing.T) {
	f := &fn{s: newStack()}
	reg := f.pushValue(storage{kind: stReg, typ: mtI64, reg: R9})
	slot := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})
	if f.refs != nil {
		t.Fatalf("owned reg/slot created refs map: %#v", f.refs)
	}
	if reg.refPrev != nil || reg.refNext != nil || slot.refPrev != nil || slot.refNext != nil {
		t.Fatalf("untracked owned storage linked refs: reg=(%p,%p) slot=(%p,%p)", reg.refPrev, reg.refNext, slot.refPrev, slot.refNext)
	}

	f.rebuildRefs()
	if len(f.refs) != 0 {
		t.Fatalf("rebuild tracked owned storage: %#v", f.refs)
	}

	f.setDepth(1)
	if got := f.refHead(refKey{kind: refSlot, id: 0}); got != nil {
		t.Fatalf("slot should remain untracked after setDepth, got %p", got)
	}
}
