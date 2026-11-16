//go:build amd64 && (wago_gc_compact_helpers || (wago_gc_compact_reads || (wago_gc_compact_alloc || (wago_gc_compact_refstore || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop)))))))) && !tinygo && !wago_target_tinygo || wago_gc_compact_refstruct

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
	if unsafe.Sizeof(*cv) != CompactCollectorViewSize || unsafe.Offsetof(v.keepTypes) != 32 {
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

func TestCompactAllocationViewCapability(t *testing.T) {
	d, _ := NewStructDesc(0, []StorageKind{StorageI32})
	for _, every := range []bool{false, true} {
		c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 256, CollectEveryAlloc: every}, []TypeDesc{d})
		if e != nil {
			t.Fatal(e)
		}
		v, e := BuildCompactInstanceView(c, []TypeID{0})
		if e != nil {
			t.Fatal(e)
		}
		cv := c.CompactView()
		enabled := !every && !failureInjectionEnabled
		if (cv.AllocationEnabled == 1) != enabled {
			t.Fatal("unexpected capability")
		}
		if enabled {
			w := c.direct.AllocationWords()
			if cv.NurseryBumpAddress != uintptr(unsafe.Pointer(w.Bump)) || cv.NurseryEnd != w.End || cv.NurseryBump != *w.Bump {
				t.Fatal("not authoritative cursor")
			}
			cv.NurseryBumpAddress++
			if ValidateCompactInstanceView(v, c) == nil {
				t.Fatal("foreign cursor accepted")
			}
			c.refreshCompactView()
		} else if cv.NurseryBumpAddress != 0 || cv.NurseryEnd != 0 {
			t.Fatal("disabled writable capability")
		}
		cv.Version = 1
		if ValidateCompactInstanceView(v, c) == nil {
			t.Fatal("ABI1 accepted")
		}
		c.refreshCompactView()
		c.Close()
		if cv.NurseryBumpAddress != 0 || cv.NurseryEnd != 0 || cv.AllocationEnabled != 0 {
			t.Fatal("closed capability exposed")
		}
	}
}

func TestCompactDirtyViewOwnership(t *testing.T) {
	d, _ := NewStructDesc(0, []StorageKind{StorageI32})
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 256}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	v, e := BuildCompactInstanceView(c, []TypeID{0})
	if e != nil {
		t.Fatal(e)
	}
	cv := c.CompactView()
	owned := c.direct.ReadView()
	if cv.Dirty == 0 || cv.Dirty != owned.Dirty || cv.DirtyCount != owned.DirtyCount {
		t.Fatal("dirty backing")
	}
	cv.Dirty++
	if ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("foreign dirty accepted")
	}
	c.refreshCompactView()
	cv.DirtyCount--
	if ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("short dirty accepted")
	}
	c.refreshCompactView()
	cv.Version = 2
	if ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("ABI2 accepted")
	}
	c.refreshCompactView()
	c.Close()
	if cv.Dirty != 0 || cv.DirtyCount != 0 {
		t.Fatal("closed dirty backing exposed")
	}
}
