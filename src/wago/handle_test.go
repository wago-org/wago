package wago

import (
	"errors"
	"testing"
)

type fakeResource struct{ closed *int }

func (r fakeResource) Close() error { *r.closed++; return nil }

func TestHandleTableInsertGetClose(t *testing.T) {
	tbl := NewHandleTable()
	closed := 0
	h := tbl.Insert("file", fakeResource{closed: &closed})

	if got, ok := tbl.Get(h, "file"); !ok || got == nil {
		t.Fatal("Get on live handle failed")
	}
	// Wrong kind is rejected.
	if _, ok := tbl.Get(h, "socket"); ok {
		t.Fatal("Get with wrong kind should fail")
	}
	if tbl.Len() != 1 {
		t.Fatalf("Len = %d, want 1", tbl.Len())
	}
	if err := tbl.Close(h); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed != 1 {
		t.Fatalf("resource Close called %d times, want 1", closed)
	}
	// Stale handle no longer resolves, and double-close is reported.
	if _, ok := tbl.Get(h, "file"); ok {
		t.Fatal("Get on closed handle should fail")
	}
	if err := tbl.Close(h); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("double Close = %v, want ErrInvalidHandle", err)
	}
}

func TestHandleTableGenerationGuard(t *testing.T) {
	tbl := NewHandleTable()
	c1, c2 := 0, 0
	h1 := tbl.Insert("timer", fakeResource{closed: &c1})
	if err := tbl.Close(h1); err != nil {
		t.Fatalf("close h1: %v", err)
	}
	// The next insert reuses the slot but with a bumped generation, so the old
	// handle must not resolve to the new resource.
	h2 := tbl.Insert("timer", fakeResource{closed: &c2})
	if h1 == h2 {
		t.Fatal("reused slot must produce a distinct handle (generation bump)")
	}
	if _, ok := tbl.Get(h1, "timer"); ok {
		t.Fatal("stale handle resolved to the reused slot")
	}
	if _, ok := tbl.Get(h2, "timer"); !ok {
		t.Fatal("fresh handle should resolve")
	}
}

func TestHandleTableCloseAll(t *testing.T) {
	tbl := NewHandleTable()
	n := 0
	for i := 0; i < 3; i++ {
		tbl.Insert("conn", fakeResource{closed: &n})
	}
	if err := tbl.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	if n != 3 {
		t.Fatalf("closed %d resources, want 3", n)
	}
	if tbl.Len() != 0 {
		t.Fatalf("Len after CloseAll = %d, want 0", tbl.Len())
	}
}
