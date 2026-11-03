//go:build wagodebug

package gc

import (
	"errors"
	"reflect"
	"testing"
)

func TestRFWGCMatureStage5MetadataRollback(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 8192, NurseryRegionShift: 6, DisableMovingNursery: true, ThroughputHeapBytes: 16384, ThroughputPageBytes: 4096, VerifyAfterCollect: true, PoisonFreed: true})
	roots := make([]Root, 130)
	for i := range roots {
		r, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		roots[i] = Root(r)
		if err = c.StructSet(r, 1, I64Value(int64(i))); err != nil {
			t.Fatal(err)
		}
	}
	before := append([]handleEntry(nil), c.handles...)
	source := append([]byte(nil), c.nursery...)
	bump := c.nurseryBump
	cleanup := armFailure(&c.throughput, failMatureMetadata, 1)
	err := c.CollectMinor(stressRootSlots(roots))
	cleanup()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("metadata failure: %v", err)
	}
	if !reflect.DeepEqual(before, c.handles) || !reflect.DeepEqual(source, c.nursery) || bump != c.nurseryBump {
		t.Fatal("source publication during failed metadata admission")
	}
	stage5Occupancy(t, &c.throughput, 0, 0, 0)
	if c.throughput.bump != 0 || len(c.throughput.mem) != 0 || c.throughput.mature.admission || !c.throughput.mature.pressure {
		t.Fatal("incomplete metadata rollback")
	}
	if err = c.Verify(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
	if err = c.CollectMinor(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
	stage5Occupancy(t, &c.throughput, 130*32, 0, 0)
	for i, r := range roots {
		v, err := c.StructGet(Ref(r), 1)
		if err != nil || v.I64() != int64(i) {
			t.Fatal("retry payload", i, err)
		}
	}
}

func TestRFWGCMatureStage5LateRollback(t *testing.T) {
	for _, point := range []failurePoint{failPromotionDestination, failPromotionCommit} {
		c := stage4Collector(t, 6)
		parent := regionStruct(t, c, 1, EmptyRoots{})
		roots := Slots{(*Root)(&parent)}
		if err := c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		child := regionStruct(t, c, 1, roots)
		grandchild := regionStruct(t, c, 0, roots)
		if err := c.StructSet(child, 0, RefValue(grandchild)); err != nil {
			t.Fatal(err)
		}
		if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
			t.Fatal(err)
		}
		before := append([]handleEntry(nil), c.handles...)
		source := append([]byte(nil), c.nursery...)
		cleanup := armFailure(c, point, 1)
		err := c.CollectMinor(roots)
		cleanup()
		if !errors.Is(err, errInjectedFailure) {
			t.Fatalf("late failure: %v", err)
		}
		if !reflect.DeepEqual(before, c.handles) || !reflect.DeepEqual(source, c.nursery) {
			t.Fatal("partial handle/source publication")
		}
		stage5Occupancy(t, &c.throughput, 32, 0, 0)
		if err = c.Verify(roots); err != nil {
			t.Fatal(err)
		}
		if err = c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		stage5Occupancy(t, &c.throughput, 96, 0, 0)
		if c.entry(grandchild).space != spaceOld {
			t.Fatal("lost consumed remembered edge on retry")
		}
	}
}

func TestRFWGCMatureStage5LargePreparationFailures(t *testing.T) {
	for _, point := range []failurePoint{failMatureMetadata, failBackingGrowth, failHandlePublication} {
		c := stage4Collector(t, 6)
		var target any = &c.throughput
		if point == failHandlePublication {
			target = c
		}
		cleanup := armFailure(target, point, 0)
		_, err := c.NewArrayDefault(2, 2048)
		cleanup()
		if !errors.Is(err, errInjectedFailure) {
			t.Fatalf("point %d: %v", point, err)
		}
		stage5Occupancy(t, &c.throughput, 0, 0, 0)
		if c.throughput.bump != 0 || len(c.throughput.mem) != 0 || c.throughput.mature.admission {
			t.Fatal("large failure leaked reservation")
		}
		r, err := c.NewArrayDefault(2, 2048)
		if err != nil {
			t.Fatal(err)
		}
		stage5Occupancy(t, &c.throughput, 8208, 0, 8208)
		if c.entry(r).off != 0 {
			t.Fatal("retry lost extent")
		}
	}
}

func TestRFWGCMatureStage5RetirementRetry(t *testing.T) {
	c := stage4Collector(t, 6)
	if _, err := c.NewArrayDefault(2, 2048); err != nil {
		t.Fatal(err)
	}
	cleanup := armFailure(&c.throughput, failThroughputReconciliation, 0)
	err := c.CollectFull(EmptyRoots{})
	cleanup()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("retirement failure: %v", err)
	}
	if len(c.throughput.pendingFree) == 0 {
		t.Fatal("lost retirement debt")
	}
	stage5Occupancy(t, &c.throughput, 0, 0, 0)
	if err = c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.throughput.bump != 0 {
		t.Fatal("retirement retry failed")
	}
}

func TestRFWGCMatureStage5HighCardsRollback(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, NurseryRegionShift: 6, DisableMovingNursery: true, ThroughputHeapBytes: 262144, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	var pad, array Ref
	roots := Slots{(*Root)(&pad), (*Root)(&array)}
	var err error
	pad, err = c.NewArrayDefaultWithRoots(2, 16380, roots)
	if err != nil {
		t.Fatal(err)
	}
	array, err = c.NewArrayDefaultWithRoots(3, 10000, roots)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	if c.entry(array).off != 65536 {
		t.Fatal("not a high card fixture")
	}
	child := regionStruct(t, c, 0, roots)
	if err = c.StructSet(child, 1, I64Value(42)); err != nil {
		t.Fatal(err)
	}
	if err = c.ArraySet(array, 9999, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	slotChild := regionStruct(t, c, 0, roots)
	slot := c.NewGlobalSlot(slotChild)
	entry := *c.entry(child)
	slotEntry := *c.entry(slotChild)
	largeEntry := *c.entry(array)
	cleanup := armFailure(c, failPromotionCommit, 1)
	err = c.CollectMinor(roots)
	cleanup()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatal("precommit did not fail", err)
	}
	if *c.entry(child) != entry || *c.entry(slotChild) != slotEntry || *c.entry(array) != largeEntry {
		t.Fatal("handle rollback changed source")
	}
	stage5Occupancy(t, &c.throughput, 65536+40016, 0, 65536+40016)
	if err = c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	if err = c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	if c.GlobalSlot(slot) != slotChild || c.entry(slotChild).space != spaceOld || c.entry(child).space != spaceOld {
		t.Fatal("lost root/card evidence")
	}
	v, err := c.StructGet(child, 1)
	if err != nil || v.I64() != 42 {
		t.Fatal("lost high-card child")
	}
	stage5Occupancy(t, &c.throughput, 65536+40016+64, 0, 65536+40016)
}
