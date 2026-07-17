package gc

import (
	"fmt"
	"testing"
)

func TestLargeStructInitializationRemembersNurseryChild(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	fields := make([]StorageKind, 16)
	for i := range fields {
		fields[i] = StorageRefNull
	}
	large, err := NewStructDesc(1, fields)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{LargeObjectBytes: 64}, []TypeDesc{leaf, large})
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]Value, len(fields))
	values[0] = RefValue(child)
	for i := 1; i < len(values); i++ {
		values[i] = RefValue(Null())
	}
	parent, err := c.NewStructWithRoots(1, values, Slots{})
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(parent).space != spaceLarge {
		t.Fatalf("parent space = %v, want large", c.entry(parent).space)
	}
	if c.RememberedCount() != 1 {
		t.Fatalf("fresh large parent remembered = %d, want 1", c.RememberedCount())
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	got, err := c.StructGet(parent, 0)
	if err != nil || got.Ref != child || c.entry(child).space != spaceOld {
		t.Fatalf("child after minor = %v/%v space=%v, want retained old %v", got.Ref, err, c.entry(child).space, child)
	}
	if c.RememberedCount() != 0 {
		t.Fatalf("remembered set after child promotion = %d, want 0", c.RememberedCount())
	}
}

func TestMinorCollectionScansNurseryNotOldLiveGraph(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 8 << 20})
	for i := 0; i < 10_000; i++ {
		old, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ForcePromote(old); err != nil {
			t.Fatal(err)
		}
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(child)
	before := c.Stats()
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	after := c.Stats()
	if got := after.MinorObjectsScanned - before.MinorObjectsScanned; got != 1 {
		t.Fatalf("minor object scans = %d, want nursery-only 1", got)
	}
	if got := after.MinorRememberedScanned - before.MinorRememberedScanned; got != 0 {
		t.Fatalf("minor remembered scans = %d, want 0", got)
	}
}

func minorBenchmarkTypes(b testing.TB) []TypeDesc {
	b.Helper()
	pf, err := NewStructDesc(0, []StorageKind{StorageI32, StorageI64})
	if err != nil {
		b.Fatal(err)
	}
	pair, err := NewStructDesc(1, []StorageKind{StorageRefNull, StorageRefNull})
	if err != nil {
		b.Fatal(err)
	}
	ia, err := NewArrayDesc(2, StorageI32)
	if err != nil {
		b.Fatal(err)
	}
	ra, err := NewArrayDesc(3, StorageRefNull)
	if err != nil {
		b.Fatal(err)
	}
	return []TypeDesc{pf, pair, ia, ra}
}

func BenchmarkMinorCollectionOldGraphScaling(b *testing.B) {
	for _, oldCount := range []int{0, 100, 10_000} {
		b.Run(fmt.Sprintf("old=%d", oldCount), func(b *testing.B) {
			c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 16 << 20}, minorBenchmarkTypes(b))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for i := 0; i < oldCount; i++ {
				old, err := c.NewStructDefault(0)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.ForcePromote(old); err != nil {
					b.Fatal(err)
				}
			}
			before := c.Stats()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				child, err := c.NewStructDefault(0)
				if err != nil {
					b.Fatal(err)
				}
				root := Root(child)
				if err := c.CollectMinor(Slots{&root}); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			after := c.Stats()
			b.ReportMetric(float64(after.MinorObjectsScanned-before.MinorObjectsScanned)/float64(b.N), "nursery-scans/op")
			b.ReportMetric(float64(after.MinorRememberedScanned-before.MinorRememberedScanned)/float64(b.N), "remembered-scans/op")
		})
	}
}
