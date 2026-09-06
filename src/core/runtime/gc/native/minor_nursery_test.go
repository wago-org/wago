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

func TestSuccessfulMinorCollectionResetsEvacuatedNurseryMetadata(t *testing.T) {
	c := newTestCollector(t, Config{PoisonFreed: true, VerifyAfterCollect: true})
	parent, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	dead, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	deadHandle := handleOf(dead)
	if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 1 || c.CardCount() != 1 {
		t.Fatalf("pre-collection remembered/cards = %d/%d, want 1/1", c.RememberedCount(), c.CardCount())
	}

	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.nurseryBump != 0 || len(c.nurseryHandles) != 0 {
		t.Fatalf("evacuated nursery bump/handles = %d/%d, want 0/0", c.nurseryBump, len(c.nurseryHandles))
	}
	if c.RememberedCount() != 0 || c.CardCount() != 0 || c.handles[handleOf(parent)].remembered {
		t.Fatalf("post-collection remembered/cards/bit = %d/%d/%v, want 0/0/false", c.RememberedCount(), c.CardCount(), c.handles[handleOf(parent)].remembered)
	}
	if got := c.handles[handleOf(child)].space; got != spaceOld {
		t.Fatalf("survivor space = %v, want old", got)
	}
	if got := c.handles[deadHandle].space; got != spaceFree {
		t.Fatalf("dead nursery space = %v, want free", got)
	}
	for h, e := range c.handles {
		if e.space == spaceNursery {
			t.Fatalf("handle %d remains in nursery", h)
		}
	}
}

func TestSuccessfulMinorCollectionWithNoSurvivorsRecyclesNursery(t *testing.T) {
	c := newTestCollector(t, Config{PoisonFreed: true, VerifyAfterCollect: true})
	const count = 128
	handles := make([]uint32, 0, count)
	for i := 0; i < count; i++ {
		r, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		handles = append(handles, handleOf(r))
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if c.nurseryBump != 0 || len(c.nurseryHandles) != 0 || len(c.promotionScratch) != 0 {
		t.Fatalf("post-collection bump/handles/plans = %d/%d/%d, want 0/0/0", c.nurseryBump, len(c.nurseryHandles), len(c.promotionScratch))
	}
	for _, h := range handles {
		if c.handles[h].space != spaceFree || c.mark[h] {
			t.Fatalf("dead handle %d state = space %v mark %v, want free/false", h, c.handles[h].space, c.mark[h])
		}
	}
	beforeHandleCount := len(c.handles)
	reused, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.handles) != beforeHandleCount {
		t.Fatalf("reusing evacuated nursery handle grew table from %d to %d", beforeHandleCount, len(c.handles))
	}
	reusedHandle := handleOf(reused)
	found := false
	for _, h := range handles {
		found = found || h == reusedHandle
	}
	if !found {
		t.Fatalf("allocation used new handle %d instead of an evacuated handle", reusedHandle)
	}
}

func TestMinorCollectionNoRootsNoSurvivorsDoesNotAllocate(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1 << 20})
	allocs := testing.AllocsPerRun(100, func() {
		for i := 0; i < 64; i++ {
			if _, err := c.NewStructDefault(0); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.CollectMinor(nil); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("no-root/no-survivor minor cycle allocations = %v, want 0", allocs)
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
			if err := c.CollectMinor(nil); err != nil {
				b.Fatal(err)
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

func BenchmarkMinorCollectionCleanupScaling(b *testing.B) {
	const nurseryObjects = 64
	for _, oldCount := range []int{0, 10_000} {
		for _, survivors := range []int{0, 1, 6} {
			name := fmt.Sprintf("old=%d/survivors=%d-of-%d", oldCount, survivors, nurseryObjects)
			b.Run(name, func(b *testing.B) {
				c, err := NewCollector(Config{
					NurseryBytes:        1 << 20,
					ThroughputHeapBytes: 16 << 20,
				}, minorBenchmarkTypes(b))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				oldRootValues := make([]Root, oldCount)
				oldRoots := make(Slots, oldCount)
				for i := 0; i < oldCount; i++ {
					old, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					if err := c.ForcePromote(old); err != nil {
						b.Fatal(err)
					}
					oldRootValues[i] = Root(old)
					oldRoots[i] = &oldRootValues[i]
				}
				if err := c.CollectMinor(nil); err != nil {
					b.Fatal(err)
				}
				rootValues := make([]Root, survivors)
				roots := make(Slots, survivors)
				for i := range roots {
					roots[i] = &rootValues[i]
				}
				var collectionRoots RootSet
				if survivors != 0 {
					collectionRoots = roots
				}

				b.ReportAllocs()
				b.ResetTimer()
				b.StopTimer()
				for i := 0; i < b.N; i++ {
					if i != 0 && i%1024 == 0 {
						if err := c.CollectFull(oldRoots); err != nil {
							b.Fatal(err)
						}
					}
					for j := 0; j < nurseryObjects; j++ {
						r, err := c.NewStructDefault(0)
						if err != nil {
							b.Fatal(err)
						}
						if j < survivors {
							rootValues[j] = Root(r)
						}
					}
					b.StartTimer()
					if err := c.CollectMinor(collectionRoots); err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					for j := range rootValues {
						rootValues[j] = Root(Null())
					}
				}
			})
		}
	}
}

func BenchmarkMinorCollectionRememberedParentScaling(b *testing.B) {
	for _, length := range []uint32{1, 1024, 16_384} {
		b.Run(fmt.Sprintf("elements=%d", length), func(b *testing.B) {
			c, err := NewCollector(Config{
				NurseryBytes:        1 << 20,
				ThroughputHeapBytes: 16 << 20,
			}, minorBenchmarkTypes(b))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			parent, err := c.NewArrayDefault(3, length)
			if err != nil {
				b.Fatal(err)
			}
			if err := c.ForcePromote(parent); err != nil {
				b.Fatal(err)
			}
			if err := c.CollectMinor(nil); err != nil {
				b.Fatal(err)
			}
			parentRoot := Root(parent)
			fullRoots := Slots{&parentRoot}
			before := c.Stats()

			b.ReportAllocs()
			b.ResetTimer()
			b.StopTimer()
			for i := 0; i < b.N; i++ {
				if i != 0 && i%1024 == 0 {
					if err := c.CollectFull(fullRoots); err != nil {
						b.Fatal(err)
					}
				}
				child, err := c.NewStructDefault(0)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if err := c.CollectMinor(nil); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				if err := c.ArraySet(parent, 0, RefValue(Null())); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			after := c.Stats()
			b.ReportMetric(float64(after.MinorRememberedScanned-before.MinorRememberedScanned)/float64(b.N), "remembered-scans/op")
		})
	}
}
