//go:build amd64 && wago_gc_compact_age2 && wago_gc_compact_helpers && !wago_gc_compact_cohort && !wago_gc_compact_shadow && !tinygo && !wago_target_tinygo

package gc

import (
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
