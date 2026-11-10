//go:build amd64

package amd64

import (
	"testing"

	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestMaterializeTrapsBeforePreservesPureTreesAndSlots(t *testing.T) {
	f := &fn{a: &encoderamd64.Asm{}, s: newStack(), sc: newScratch()}
	f.pushValue(storage{kind: stLocalRef, typ: mtI32})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	f.pushBinOp(opAdd, mtI32)
	pure := f.s.back()
	f.pushValue(storage{kind: stLocalRef, typ: mtI32})
	f.pushUnOp(opEqz, mtI32)
	pureEqz := f.s.back()
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
	f.pushValue(storage{kind: stConst, typ: mtI32})
	f.pushBinOp(opDivU, mtI32)
	trapping := f.s.back()
	argument := f.pushValue(storage{kind: stSlot, typ: mtI32, slot: 0})
	f.materializeTrapsBefore(argument)
	if !pure.isDeferred() || !pureEqz.isDeferred() {
		t.Fatal("pure prefix tree was eagerly materialized")
	}
	if !trapping.isValue() || trapping.st.kind != stReg {
		t.Fatalf("trapping prefix was not materialized: %+v", trapping.st)
	}
	if argument.st.kind != stSlot || argument.st.slot != 0 {
		t.Fatalf("argument slot changed before capture: %+v", argument.st)
	}
	if !registerCallArgNeedsCapture(argument) {
		t.Fatal("slot-backed argument must still be captured before canonical flush")
	}
	if f.depth() != 4 {
		t.Fatalf("operand depth changed: %d", f.depth())
	}
}

func TestPopBranchConditionProtectsOwnedAndBorrowedRegisters(t *testing.T) {
	for _, kind := range []storageKind{stReg, stLocalReg, stGlobReg} {
		for _, reg := range []Reg{RAX, RCX, RDX, R12} {
			f := &fn{a: &encoderamd64.Asm{}, s: newStack()}
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
			if kind == stReg {
				f.pushReg(reg, mtI32)
			} else {
				f.pinnedLocalMask = maskOf(reg)
				f.pushValue(storage{kind: kind, typ: mtI32, reg: reg})
			}
			condition, owned := f.popBranchCondition()
			if condition == RAX || condition == RCX || condition == RDX || !f.pinned.has(condition) {
				t.Fatalf("kind %v register %v: unprotected condition %v", kind, reg, condition)
			}
			if owned != (kind == stReg || condition != reg) {
				t.Fatalf("kind %v register %v: wrong ownership for %v", kind, reg, condition)
			}
			if f.depth() != 1 {
				t.Fatalf("operand depth changed: %d", f.depth())
			}
		}
	}
}
