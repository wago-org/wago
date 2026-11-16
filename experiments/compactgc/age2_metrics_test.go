//go:build wago_gc_compact_age2 && wago_gc_compact_age2_metrics && !wago_gc_compact_cohort && !wago_gc_compact_shadow

package compactgc

import "testing"

func TestAge2MetricsSurvivalAndOverflow(t *testing.T) {
	c := ageHeap(t, 128, 256, 16, node)
	a, b, p := obj(t, c, 0), obj(t, c, 0), obj(t, c, 0)
	if e := c.Minor([]*Ref{&a, &b, &p}); e != nil {
		t.Fatal(e)
	}
	m, ok := c.Age2Metrics()
	if !ok {
		t.Fatal("missing metrics")
	}
	d := m.Last
	if d.FreshCapacityBytes != 64 || d.FreshAllocatedBytes != 48 || d.FreshAllocatedObjects != 3 || d.FirstSurvivorBytes != 48 || d.RetainedPrefixOutBytes != 16 || d.OverflowPromotedBytes != 32 {
		t.Fatalf("first %+v", d)
	}
	set(t, c, p, 0, a)
	if e := c.Minor([]*Ref{&p}); e != nil {
		t.Fatal(e)
	}
	m, _ = c.Age2Metrics()
	d = m.Last
	if d.FreshCapacityBytes != 48 || d.FreshAllocatedBytes != 0 || d.RetainedPrefixInBytes != 16 || d.Age1PromotedBytes != 16 || d.RememberedParents != 1 || d.RememberedEdges != 2 {
		t.Fatalf("second %+v", d)
	}
	if m.MinorSuccesses != 2 || m.MinorTotals.FreshAllocatedObjects != 3 {
		t.Fatal(m)
	}
	if again, _ := c.Age2Metrics(); again != m {
		t.Fatal("snapshot mutated state")
	}
}
func TestAge2MetricsFailedAttemptAndOldDirect(t *testing.T) {
	c := ageHeap(t, 128, 256, 32, Descriptor{Kind: KindStruct, Align: 4, PayloadBytes: 68})
	r := obj(t, c, 0)
	bad := Ref(48)
	if e := c.Minor([]*Ref{&bad}); e == nil {
		t.Fatal("bad root")
	}
	m, _ := c.Age2Metrics()
	if m.MinorFailures != 1 || m.MinorTotals.OldDirectAllocatedBytes != 0 || m.Last != (Age2CycleMetrics{}) {
		t.Fatal(m)
	}
	if e := c.Minor([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	m, _ = c.Age2Metrics()
	if m.Last.OldDirectAllocatedBytes != 80 || m.Last.OldDirectAllocatedObjects != 1 {
		t.Fatal(m)
	}
	if e := c.Full([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	m, _ = c.Age2Metrics()
	if m.Last.OldDirectAllocatedBytes != 0 || m.Last.FullOldLiveBytes != 80 || m.FullSuccesses != 1 {
		t.Fatal(m)
	}
	if e := c.Minor([]*Ref{&r}); e != nil {
		t.Fatal(e)
	}
	m, _ = c.Age2Metrics()
	if m.Last.OldDirectAllocatedBytes != 0 || m.MinorTotals.OldDirectAllocatedObjects != 1 {
		t.Fatal("double count after flip", m)
	}
}
