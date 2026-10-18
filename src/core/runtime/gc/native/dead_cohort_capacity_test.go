//go:build amd64 && !tinygo

package gc

import "testing"

func TestDeadCohortExactCapacityMixedRecycling(t *testing.T) {
	const count = 512
	leaf, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	array, err := NewArrayDesc(1, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 4 << 20, ThroughputHeapBytes: 16 << 20, LargeObjectBytes: 512, VerifyAfterCollect: true}, []TypeDesc{leaf, array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for range count {
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if len(c.freeHandles) != count || cap(c.freeHandles) != count {
		t.Fatalf("pure cohort free length/capacity = %d/%d, want %d/%d", len(c.freeHandles), cap(c.freeHandles), count, count)
	}
	entries := len(c.handles)
	large, err := c.NewArrayDefault(1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if e := c.entry(large); e.space != spaceLarge || !e.young() {
		t.Fatal("fixture requires a young large object")
	}
	for range count - 1 {
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.handles) != entries || len(c.nurseryHandles) != count || len(c.freeHandles) != 0 || cap(c.freeHandles) != count {
		t.Fatalf("reused cohort entries=%d/%d nursery=%d free=%d/%d", len(c.handles), entries, len(c.nurseryHandles), len(c.freeHandles), cap(c.freeHandles))
	}
	if err := c.CollectMinor(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if len(c.freeHandles) != count {
		t.Fatalf("mixed cohort free length = %d, want %d", len(c.freeHandles), count)
	}
	if *c.entry(large) != (handleEntry{}) {
		t.Fatal("large object ownership was not released")
	}
	t.Logf("mixed cohort free length/capacity = %d/%d", len(c.freeHandles), cap(c.freeHandles))
	if cap(c.freeHandles) != count {
		t.Fatalf("mixed cohort grew free capacity to %d, want %d", cap(c.freeHandles), count)
	}
}
