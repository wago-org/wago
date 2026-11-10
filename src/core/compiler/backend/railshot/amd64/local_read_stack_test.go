//go:build amd64

package amd64

import "testing"

func TestLocalReadStackMatchesGenericConstruction(t *testing.T) {
	direct, generic := newStackWithCap(minStackArenaCap), newStackWithCap(minStackArenaCap)
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < 2048; i++ {
			kind, reg := stLocalRef, Reg(0)
			if i%2 != 0 {
				kind, reg = stLocalReg, Reg(i%16)
			}
			typ := machineType(1 + i%int(mtV128))
			index := uint32(i * 65537)
			a := direct.pushLocalRead(kind, typ, reg, index)
			b := generic.pushValue(storage{kind: kind, typ: typ, reg: reg, idx: index})
			if a.st != b.st || a.arg0 != nil || a.arg1 != nil {
				t.Fatalf("pass=%d node=%d: direct=%+v generic=%+v", pass, i, a.st, b.st)
			}
			if direct.logicalDepth != generic.logicalDepth || direct.pendingEffects != generic.pendingEffects ||
				direct.canonicalSlots != generic.canonicalSlots || direct.hasGCRoots != generic.hasGCRoots ||
				direct.spillExtent != generic.spillExtent || direct.spillExtentValid != generic.spillExtentValid {
				t.Fatalf("pass=%d node=%d: stack summaries differ", pass, i)
			}
			if i%3 == 0 {
				direct.erase(a)
				generic.erase(b)
			}
		}
		direct.reset()
		generic.reset()
	}
}
