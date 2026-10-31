//go:build amd64

package amd64

import (
	"math/rand"
	"testing"
)

func TestDeferredTreeLabelsMatchSeparateUpdates(t *testing.T) {
	rng := rand.New(rand.NewSource(50))
	for i := 0; i < 8192; i++ {
		left := elem{st: storage{kind: deferredStorageKind, slot: rng.Uint32()}}
		right := elem{st: storage{kind: deferredStorageKind, slot: rng.Uint32()}}
		if i%7 == 0 {
			left.st.kind = stConst
		}
		if i%11 == 0 {
			right.st.kind = stLocalRef
		}
		if i%13 == 0 {
			left.st.slot &^= 0x00ff0000
			left.arg0, left.arg1 = &right, &elem{st: storage{kind: stLocalRef}}
		}
		root := elem{st: storage{kind: deferredStorageKind, slot: rng.Uint32(), meta: uint8(rng.Intn(256)), cval: rng.Int63()}, arg0: &left, arg1: &right}
		if i%3 == 0 {
			root.arg1 = nil
		}
		if i%17 == 0 {
			root.arg0 = nil
		}
		reference := root
		reference.setDeferredDepth(1 + max16(deferDepthOf(reference.arg0), deferDepthOf(reference.arg1)))
		reference.setRegisterNeed(combineRegisterNeed(treeRegisterNeed(reference.arg0), treeRegisterNeed(reference.arg1), reference.arg1 != nil))
		labelDeferredNode(&root)
		if root != reference {
			t.Fatalf("case %d: combined=%+v separate=%+v", i, root, reference)
		}
	}
}
