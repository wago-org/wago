//go:build amd64 && wago_gc_compact_age2 && wago_gc_compact_helpers && !wago_gc_compact_cohort && !wago_gc_compact_shadow && !tinygo && !wago_target_tinygo

package gc

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestCompactAge2ViewFlipsAndPolicy(t *testing.T) {
	d, _ := NewStructDesc(0, []StorageKind{StorageI32})
	c, e := NewExperimentalCompactCollector(Config{ExperimentalCompactAge2: true, NurseryBytes: 128, ThroughputHeapBytes: 1024}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	total, active, cap := c.direct.YoungBudget()
	if total != 128 || active != 64 || cap != 32 {
		t.Fatal(total, active, cap)
	}
	v, e := BuildCompactInstanceView(c, []TypeID{0})
	if e != nil {
		t.Fatal(e)
	}
	cv := c.CompactView()
	first := cv.NurseryStart
	if cv.Version != 4 || cv.Policy != CompactPolicyAge2 || unsafe.Sizeof(*cv) != 88 {
		t.Fatal(*cv)
	}
	r, e := c.NewStructDefault(0)
	if e != nil {
		t.Fatal(e)
	}
	root := Root(r)
	if e = c.CollectMinor(Slots{&root}); e != nil {
		t.Fatal(e)
	}
	if cv.NurseryStart == first || !c.direct.IsYoung(directRef(Ref(root))) {
		t.Fatal("first survivor not retained after flip")
	}
	if e = ValidateCompactInstanceView(v, c); e != nil {
		t.Fatal(e)
	}
	if e = c.CollectMinor(Slots{&root}); e != nil {
		t.Fatal(e)
	}
	if cv.NurseryStart != first || c.direct.IsYoung(directRef(Ref(root))) {
		t.Fatal("second survivor not promoted")
	}
	if c.Stats().Allocations != 1 {
		t.Fatal(c.Stats())
	}
	cv.Policy = 99
	if ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("unknown policy accepted")
	}
	c.refreshCompactView()
	if e = ValidateCompactInstanceView(v, c); e != nil {
		t.Fatal(e)
	}
	c.Close()
	if cv.Arena != 0 || cv.AllocationEnabled != 0 || cv.Policy != CompactPolicyPromoteAll {
		t.Fatal("close exposed active policy/backing")
	}
}

func TestCompactAge2RejectsYoungGeometry(t *testing.T) {
	for _, n := range []uint32{16, 32, 65, 96 + 16} {
		if _, e := NewExperimentalCompactCollector(Config{ExperimentalCompactAge2: true, NurseryBytes: n, ThroughputHeapBytes: 1024}, nil); e == nil {
			t.Fatal("accepted", n)
		}
	}
}

func TestCompactAge2RoundedCapAndDefaultBudget(t *testing.T) {
	for _, age2 := range []bool{false, true} {
		c, e := NewExperimentalCompactCollector(Config{ExperimentalCompactAge2: age2, NurseryBytes: 96, ThroughputHeapBytes: 256}, nil)
		if e != nil {
			t.Fatal(e)
		}
		total, active, cap := c.direct.YoungBudget()
		if total != 96 || (age2 && (active != 48 || cap != 16)) || (!age2 && (active != 96 || cap != 0)) {
			t.Fatal(total, active, cap)
		}
		if c.direct.FixedBytes() == 0 {
			t.Fatal("missing heap accounting")
		}
		c.Close()
	}
}

func TestCompactAge2NativePublicationAfterBothFlips(t *testing.T) {
	if failureInjectionEnabled {
		t.Skip("native capability intentionally unavailable")
	}
	d, _ := NewStructDesc(0, []StorageKind{StorageI32})
	c, e := NewExperimentalCompactCollector(Config{ExperimentalCompactAge2: true, NurseryBytes: 128, ThroughputHeapBytes: 1024}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	v, e := BuildCompactInstanceView(c, []TypeID{0})
	if e != nil {
		t.Fatal(e)
	}
	first := c.CompactView().NurseryStart
	for i := uint32(0); i < 4; i++ {
		cv := c.CompactView()
		w := c.direct.AllocationWords()
		if cv.AllocationEnabled != 1 || cv.NurseryBumpAddress != uintptr(unsafe.Pointer(w.Bump)) || *w.Bump != cv.NurseryBump || w.End != cv.NurseryEnd {
			t.Fatal("stale writable view")
		}
		off := *w.Bump
		arena := unsafe.Slice((*byte)(unsafe.Pointer(cv.Arena)), int(cv.ArenaBytes))
		starts := unsafe.Slice((*byte)(unsafe.Pointer(cv.Starts)), int(cv.StartCount))
		clear(arena[off : off+16])
		binary.LittleEndian.PutUint32(arena[off:], 0)
		binary.LittleEndian.PutUint32(arena[off+4:], 100+i)
		starts[off/8] = 1
		*w.Bump = off + 16
		cv.NurseryBump = off + 16
		native := Root(Ref(off))
		helper, e := c.NewStructDefaultWithRoots(0, Slots{&native})
		if e != nil {
			t.Fatal(e)
		}
		other := Root(helper)
		if e = c.CollectMinor(Slots{&native, &other}); e != nil {
			t.Fatal(e)
		}
		wantStart := first
		if i%2 == 0 {
			wantStart = first + 64
		}
		if cv.NurseryStart != wantStart {
			t.Fatal("did not alternate", i, *cv)
		}
		val, e := c.StructGet(Ref(native), 0)
		if e != nil || val.Bits != uint64(100+i) {
			t.Fatal(val, e)
		}
		if e = ValidateCompactInstanceView(v, c); e != nil {
			t.Fatal(e)
		}
		if c.Stats().Allocations != uint64(2*(i+1)) {
			t.Fatal("deferred count included survivors", c.Stats())
		}
	}
	v.Version = 3
	if ValidateCompactInstanceView(v, c) == nil {
		t.Fatal("ABI3 accepted")
	}
}

func TestCompactAge2OldParentRetainedYoungBarrier(t *testing.T) {
	box, _ := NewStructDesc(0, []StorageKind{StorageRefNull})
	leaf, _ := NewStructDesc(1, []StorageKind{StorageI32})
	c, e := NewExperimentalCompactCollector(Config{ExperimentalCompactAge2: true, NurseryBytes: 128, ThroughputHeapBytes: 512}, []TypeDesc{box, leaf})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	p, e := c.NewStructDefault(0)
	if e != nil {
		t.Fatal(e)
	}
	parent := Root(p)
	for i := 0; i < 2; i++ {
		if e = c.CollectMinor(Slots{&parent}); e != nil {
			t.Fatal(e)
		}
	}
	child, e := c.NewStructWithRoots(1, []Value{{Kind: StorageI32, Bits: 77}}, Slots{&parent})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.StructSet(Ref(parent), 0, RefValue(child)); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = c.CollectMinor(Slots{&parent}); e != nil {
			t.Fatal(e)
		}
		edge, e := c.StructGet(Ref(parent), 0)
		if e != nil {
			t.Fatal(e)
		}
		got, e := c.StructGet(edge.Ref, 0)
		if e != nil || got.Bits != 77 {
			t.Fatal(got, e)
		}
		if c.direct.IsYoung(directRef(edge.Ref)) != (i == 0) {
			t.Fatal("wrong child age")
		}
	}
}

func TestCompactAge2OversizedFullRetry(t *testing.T) {
	arr, _ := NewArrayDesc(0, StorageI64)
	c, e := NewExperimentalCompactCollector(Config{ExperimentalCompactAge2: true, NurseryBytes: 128, ThroughputHeapBytes: 128}, []TypeDesc{arr})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	//80-byte objects exceed the64-byte ACTIVE space, not the128-byte total budget.
	for i := 0; i < 3; i++ {
		r, e := c.NewArrayDefaultWithRoots(0, 9, EmptyRoots{})
		if e != nil {
			t.Fatal(e)
		}
		if c.direct.IsYoung(directRef(r)) {
			t.Fatal("oversized object entered active nursery")
		}
	}
	if c.Stats().FullCollections == 0 || c.Stats().Allocations != 3 {
		t.Fatal("no full retry", c.Stats())
	}
	rootRef, e := c.NewArrayDefaultWithRoots(0, 9, EmptyRoots{})
	if e != nil {
		t.Fatal(e)
	}
	root := Root(rootRef)
	before := Ref(root)
	if _, e = c.NewArrayDefaultWithRoots(0, 9, Slots{&root}); e == nil {
		t.Fatal("live old capacity unexpectedly fit")
	}
	if _, e = c.ArrayLen(Ref(root)); e != nil {
		t.Fatal("failed retry lost root", before, root, e)
	}
}

func TestCompactAge2ChecksActiveArrayCapacity(t *testing.T) {
	arr, _ := NewArrayDesc(0, StorageI64)
	c, e := NewExperimentalCompactCollector(Config{ExperimentalCompactAge2: true, NurseryBytes: 128, ThroughputHeapBytes: 32}, []TypeDesc{arr})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if c.CheckArrayAllocation(0, 9) == nil {
		t.Fatal("counted inactive half as allocation capacity")
	}
	if e = c.CheckArrayAllocation(0, 7); e != nil {
		t.Fatal("64-byte active allocation rejected", e)
	}
}
