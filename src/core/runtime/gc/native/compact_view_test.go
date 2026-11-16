//go:build amd64 && (wago_gc_compact_helpers || wago_gc_compact_reads) && !tinygo && !wago_target_tinygo

package gc

import (
	"testing"
	"unsafe"
)

func TestCompactReadViewLifecycle(t *testing.T) {
	d, _ := NewStructDesc(0, []StorageKind{StorageI32})
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 1024}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	ids := []TypeID{0}
	v, e := BuildCompactInstanceView(c, ids)
	if e != nil {
		t.Fatal(e)
	}
	ids[0] = 999
	if e = ValidateCompactInstanceView(v, c); e != nil {
		t.Fatal(e)
	}
	cv := c.CompactView()
	initial := cv.NurseryBump
	r, e := c.NewStructDefault(0)
	if e != nil {
		t.Fatal(e)
	}
	if cv != c.CompactView() || cv.NurseryBump <= initial {
		t.Fatal("view identity/bump not refreshed")
	}
	if unsafe.Sizeof(*cv) != 56 || unsafe.Offsetof(v.keepTypes) != 32 {
		t.Fatal("layout")
	}
	// A root slot is retargeted by the moving adapter, not a cached address.
	root := Root(r)
	roots := Slots{&root}
	if e = c.CollectFull(roots); e != nil {
		t.Fatal(e)
	}
	if cv.NurseryBump != initial || cv.OldBump <= cv.OldStart {
		t.Fatal("collection boundaries stale")
	}
	if e = ValidateCompactInstanceView(v, c); e != nil {
		t.Fatal(e)
	}
	saved := cv.Arena
	cv.Arena++
	if ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("foreign arena accepted")
	}
	cv.Arena = saved
	cv.NurseryBump++
	if ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("stale range accepted")
	}
	cv.NurseryBump--
	c.Close()
	if cv.Arena != 0 || cv.Starts != 0 || cv.NurseryBump != 0 {
		t.Fatal("closed backing exposed")
	}
	if c.CompactView() != nil || ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("closed view accepted")
	}
}

func TestCompactReadViewErrorAndMapOwnership(t *testing.T) {
	d, _ := NewArrayDesc(0, StorageI64)
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 256, CollectEveryAlloc: true}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	v, e := BuildCompactInstanceView(c, []TypeID{0})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = BuildCompactInstanceView(c, []TypeID{1}); e == nil {
		t.Fatal("bad map admitted")
	}
	empty, e := BuildCompactInstanceView(c, nil)
	if e != nil || ValidateCompactInstanceView(empty, c) != nil {
		t.Fatal("empty map rejected", e)
	}
	if _, e = c.NewArrayDefaultWithRoots(0, ^uint32(0), EmptyRoots{}); e == nil {
		t.Fatal("oversized allocation succeeded")
	}
	if e = ValidateCompactInstanceView(v, c); e != nil {
		t.Fatal("stale view after failure", e)
	}
	old := v.LocalTypes
	v.LocalTypes++
	if ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("foreign map accepted")
	}
	v.LocalTypes = old
	if c.DirectView() != nil {
		t.Fatal("compact heap exposed direct ABI")
	}
}
