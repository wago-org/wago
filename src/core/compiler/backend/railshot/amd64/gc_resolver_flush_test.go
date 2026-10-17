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
