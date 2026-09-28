//go:build linux && amd64

package amd64

import "testing"

func TestIntegerConstantPushMatchesGenericStorage(t *testing.T) {
	for _, typ := range []machineType{mtI32, mtI64} {
		for _, rooted := range []bool{false, true} {
			generic, literal := newStackWithCap(minStackArenaCap), newStackWithCap(minStackArenaCap)
			prefix := storage{kind: stReg, typ: mtI64, reg: Reg(1)}
			prefix.setGCRoot(rooted)
			generic.pushValue(prefix)
			literal.pushValue(prefix)
			for round := 0; round < 2; round++ {
				generic.canonicalSlots, literal.canonicalSlots = true, true
				for i := 0; i < 64; i++ {
					value := -1<<63 + int64(i)*0x100000001
					want := generic.pushValue(storage{kind: stConst, typ: typ, cval: value})
					got := literal.pushIntegerConstant(typ, value)
					if got.st != want.st || got.elemKind() != want.elemKind() {
						t.Fatalf("type=%d root=%t value=%d: literal %+v, generic %+v", typ, rooted, value, got.st, want.st)
					}
					if literal.logicalDepth != generic.logicalDepth || literal.canonicalSlots != generic.canonicalSlots || literal.hasGCRoots != generic.hasGCRoots {
						t.Fatal("literal push changed stack depth, layout, or root facts")
					}
				}
				// Reusing dirty arena chunks must not preserve previous node facts.
				generic.reset()
				literal.reset()
			}
		}
	}
}
