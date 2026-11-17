//go:build amd64

package amd64

import "testing"

// Exercise the real chunk boundary while preserving every pointer until reset.
func TestStackChunkLinksAndReset(t *testing.T) {
	s := newStack()
	count := 3*maxStackChunkCap + 17
	var first, previous *elem
	for i := 0; i < count; i++ {
		e := s.pushIntegerConstant(mtI32, int64(i))
		if first == nil {
			first = e
		}
		if previous != nil && (previous.next != e || e.prev != previous) {
			t.Fatal("physical links changed across allocation")
		}
		previous = e
	}
	if len(s.chunks) < 3 {
		t.Fatal("test did not cross chunks")
	}
	for _, chunk := range s.chunks {
		if cap(chunk) > maxStackChunkCap {
			t.Fatalf("chunk capacity %d exceeds %d", cap(chunk), maxStackChunkCap)
		}
	}
	for e, i := first, 0; i < count; i++ {
		if e == s.head || e.st.cval != int64(i) {
			t.Fatalf("node %d lost", i)
		}
		e = e.next
	}
	chunks := len(s.chunks)
	s.reset()
	if s.back() != nil || s.logicalDepth != 0 {
		t.Fatal("reset retained logical operands")
	}
	for i := 0; i < count; i++ {
		s.pushIntegerConstant(mtI32, int64(i+1))
	}
	if len(s.chunks) != chunks {
		t.Fatal("reset did not reuse backing chunks")
	}
	for e, i := s.head.next, 0; i < count; i++ {
		if e.st.cval != int64(i+1) || e.arg0 != nil || e.arg1 != nil {
			t.Fatalf("stale node %d after reset", i)
		}
		e = e.next
	}
	for s.back() != nil {
		s.erase(s.back())
	}
	if s.logicalDepth != 0 || s.head.next != s.head {
		t.Fatal("cross-chunk erase left operands")
	}
}

func TestStackChunkRecycle(t *testing.T) {
	sc := newScratchWithStackCap(defaultStackArenaCap)
	f := &sc.fnState
	f.sc, f.s = sc, sc.stack
	for i := 0; i < 3*maxStackChunkCap+17; i++ {
		f.s.pushIntegerConstant(mtI32, int64(i))
	}
	for f.s.back() != nil {
		f.s.erase(f.s.back())
	}
	f.recycleEmptyOperandArena()
	if f.s.cur != 0 || len(f.s.chunks[0]) != 1 || f.s.logicalDepth != 0 {
		t.Fatal("empty multi-chunk arena was not rewound")
	}
	if f.s.head.next != f.s.head || f.s.head.prev != f.s.head {
		t.Fatal("recycled sentinel links are invalid")
	}
	e := f.s.pushIntegerConstant(mtI32, 42)
	if e.st.cval != 42 || e.arg0 != nil || e.arg1 != nil {
		t.Fatal("recycled storage was not reusable")
	}
}
