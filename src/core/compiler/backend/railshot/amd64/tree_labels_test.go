//go:build amd64

package amd64

import (
	"math/rand"
	"testing"
)

func TestDeferredTreeLabelsMatchSeparateUpdates(t *testing.T) {
	s := newStack()

	rng := rand.New(rand.NewSource(50))
	for i := 0; i < 8192; i++ {
		left := s.testElem(elem{st: storage{kind: deferredStorageKind, slot: rng.Uint32()}})
		right := s.testElem(elem{st: storage{kind: deferredStorageKind, slot: rng.Uint32()}})
		if i%7 == 0 {
			left.st.kind = stConst
		}
		if i%11 == 0 {
			right.st.kind = stLocalRef
		}
		if i%13 == 0 {
			left.st.slot &^= 0x00ff0000
			left.arg0, left.arg1 = s.ref(right), s.ref(s.testElem(elem{st: storage{kind: stLocalRef}}))
		}
		root := elem{st: storage{kind: deferredStorageKind, slot: rng.Uint32(), meta: uint8(rng.Intn(256)), cval: rng.Int63()}, arg0: s.ref(left), arg1: s.ref(right)}
		if i%3 == 0 {
			root.arg1 = 0
		}
		if i%17 == 0 {
			root.arg0 = 0
		}
		reference := root
		reference.setDeferredDepth(1 + max16(deferDepthOf(s.node(reference.arg0)), deferDepthOf(s.node(reference.arg1))))
		reference.setRegisterNeed(combineRegisterNeed(treeRegisterNeed(s, s.node(reference.arg0)), treeRegisterNeed(s, s.node(reference.arg1)), reference.arg1 != 0))
		labelDeferredNode(s, &root)
		if root != reference {
			t.Fatalf("case %d: combined=%+v separate=%+v", i, root, reference)
		}
	}
}
