//go:build amd64

package amd64

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestOperandHandleLayout(t *testing.T) {
	if got := unsafe.Sizeof(elem{}); got != 40 {
		t.Fatalf("operand bytes=%d, want 40", got)
	}
	if got := unsafe.Sizeof(nodeID(0)); got != 4 {
		t.Fatalf("handle bytes=%d, want 4", got)
	}
}

func TestOperandHandleNavigationAcrossChunks(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	var ids []nodeID
	for i := 0; i < 25000; i++ {
		e := s.pushIntegerConstant(mtI64, int64(i))
		id := s.ref(e)
		if id == 0 || s.node(id) != e {
			t.Fatalf("identity lost at %d", i)
		}
		ids = append(ids, id)
	}
	runtime.GC()
	for i := len(ids) - 1; i >= 0; i-- {
		e := s.node(ids[i])
		if e != s.back() || e.st.cval != int64(i) || s.ref(e) != ids[i] {
			t.Fatalf("navigation lost at %d", i)
		}
		s.erase(e)
	}
	if s.node(0) != nil || s.ref(nil) != 0 || s.back() != nil {
		t.Fatal("nil or empty handle mismatch")
	}
	if s.refSearches == 0 {
		t.Fatal("cross-chunk fallback was not exercised")
	}
	runtime.KeepAlive(s)
}

func TestOperandHandleRejectsForeignPointer(t *testing.T) {
	s, other := newStack(), newStack()
	foreign := other.pushIntegerConstant(mtI32, 1)
	defer func() {
		if recover() == nil {
			t.Fatal("foreign pointer accepted")
		}
	}()
	_ = s.ref(foreign)
}

func TestOperandHandleRejectsUnusedOffset(t *testing.T) {
	s := newStack()
	defer func() {
		if recover() == nil {
			t.Fatal("unused handle accepted")
		}
	}()
	_ = s.node(2)
}

func TestOperandHandleFreshPublication(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	for i := 0; i < 25000; i++ {
		var e *elem
		switch i % 3 {
		case 0:
			e = s.pushValue(storage{kind: stConst, typ: mtI64, cval: int64(i)})
		case 1:
			e = s.pushIntegerConstant(mtI32, int64(i))
		case 2:
			e = s.pushLocalRead(stLocalRef, mtI32, regNone, uint32(i))
		}
		id := s.lastAllocatedID()
		if s.node(id) != e || s.head.prev != id || e.next != 1 || s.node(e.prev).next != id {
			t.Fatalf("fresh publication mismatch at %d", i)
		}
	}
}
