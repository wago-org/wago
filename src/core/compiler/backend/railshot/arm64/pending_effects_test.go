//go:build arm64

package arm64

import (
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestPendingTrapsPreservePureAncestors(t *testing.T) {
	f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch()}
	f.pushValue(storage{kind: stLocalRef, typ: mtI32})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	f.pushBinOp(opAdd, mtI32)
	pure := f.s.back()
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
	f.pushValue(storage{kind: stConst, typ: mtI32})
	f.pushBinOp(opDivU, mtI32)
	trapping := f.s.back()
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 3})
	f.pushBinOp(opAdd, mtI32)
	ancestor := f.s.back()
	load := f.pushValue(storage{kind: stMemRef, typ: mtI32, reg: X12, idx: 4})
	argument := f.pushValue(storage{kind: stSlot, typ: mtI32, slot: 0})
	f.materializePendingTraps()
	if !pure.isDeferred() || !ancestor.isDeferred() {
		t.Fatal("pure expression or ancestor was eagerly materialized")
	}
	if trapping.isDeferred() || trapping.st.kind != stReg {
		t.Fatalf("nested div was not materialized: %+v", trapping.st)
	}
	if argument.st.kind != stSlot || argument.st.slot != 0 {
		t.Fatal("slot-backed argument changed")
	}
	if load.st.kind != stMemRef {
		t.Fatal("explicitly bounds-checked load was eagerly materialized")
	}
	if f.depth() != 4 {
		t.Fatalf("operand depth changed: %d", f.depth())
	}
	f.guardMode = true
	f.materializePendingTraps()
	if load.st.kind != stReg {
		t.Fatal("guard-backed load was not materialized")
	}
}
