//go:build amd64

package amd64

import (
	"testing"

	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestGCResolvedObjectRemainsPinnedAfterFlush(t *testing.T) {
	for _, cacheReg := range []Reg{RBP, R12} {
		f := &fn{
			a: &x86.Asm{}, s: newStackWithCap(minStackArenaCap), sc: &scratch{},
			memSizeReg: regNone, globalCellReg: regNone,
			gcResolved: gcResolvedObject{valid: true, local: 9, typeIndex: 2, requiredBytes: 24, reg: cacheReg},
		}
		f.pinned = f.pinned.add(cacheReg)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0x4e96ea})
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: 16})
		f.markTopGCReference()
		f.flush()
		if !f.gcResolved.valid || f.gcResolved.reg != cacheReg {
			t.Fatal("ordinary operand flush discarded a valid noncollecting object cache")
		}
		if !f.pinned.has(cacheReg) {
			t.Fatalf("flush made cached raw object register %v allocatable", cacheReg)
		}
		for _, reg := range gpAlloc {
			if reg != cacheReg {
				f.reserved = f.reserved.add(reg)
			}
		}
		if reg := f.allocRegOrNone(0); reg != regNone {
			t.Fatalf("allocator overwrote cached object register %v", reg)
		}
	}
}

func TestGCResolvedObjectSpillRemainsReservedAfterFlush(t *testing.T) {
	f := &fn{
		a: &x86.Asm{}, s: newStackWithCap(minStackArenaCap), sc: &scratch{},
		memSizeReg: regNone, globalCellReg: regNone, spillFloor: 2,
	}
	f.cacheGCResolvedObjectInSpill(9, 2, 24, RAX, 2)
	if !f.gcResolved.valid || !f.gcResolved.spill || f.gcResolved.spillSlot != 2 || f.spillFloor != 3 {
		t.Fatalf("spill certificate = %+v floor=%d, want slot2 floor3", f.gcResolved, f.spillFloor)
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 0x4e96ea})
	f.flush()
	if !f.gcResolved.valid || f.spillFloor != 3 || f.allocSpillSlot() < 3 {
		t.Fatalf("flush exposed certificate slot: valid=%t floor=%d next=%d", f.gcResolved.valid, f.spillFloor, f.allocSpillSlot())
	}
	address, done := f.materializeGCResolvedObject()
	if address == regNone || !f.pinned.has(address) {
		t.Fatalf("materialized spill address reg=%d pinned=%#x", address, f.pinned)
	}
	done()
	if f.pinned.has(address) {
		t.Fatalf("materialized spill address remained pinned: reg=%d pinned=%#x", address, f.pinned)
	}
	f.invalidateGCResolvedObject()
	if f.gcResolved.valid || f.spillFloor != 2 {
		t.Fatalf("spill certificate release: valid=%t floor=%d, want false/2", f.gcResolved.valid, f.spillFloor)
	}
}
