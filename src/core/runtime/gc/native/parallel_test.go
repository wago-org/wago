//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"fmt"
	"runtime"
	"testing"
)

func parallelTestProcessors(t *testing.T) {
	t.Helper()
	previous := runtime.GOMAXPROCS(8)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
}

func newParallelTestGraph(t *testing.T, workers int, immediate bool) (*Collector, Ref, []Ref) {
	t.Helper()
	fields := []StorageKind{StorageRefNull, StorageI64, StorageRefNull, StorageExternRefNull}
	for range 28 {
		fields = append(fields, StorageI64)
	}
	node, err := NewStructDesc(0, fields)
	if err != nil {
		t.Fatal(err)
	}
	array, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Workers: workers, NurseryBytes: 8 << 20, ThroughputHeapBytes: 32 << 20, VerifyAfterCollect: true, PoisonFreed: true, DisableMovingNursery: immediate}
	if !immediate {
		cfg.SurvivorBytes = 2 << 20
	}
	c, err := NewCollector(cfg, []TypeDesc{node, array})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	const count = parallelMinObjects + 257
	parent, err := c.NewArrayDefault(1, count)
	if err != nil {
		t.Fatal(err)
	}
	refs := make([]Ref, count)
	for i := range refs {
		refs[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.ArraySet(parent, uint32(i), RefValue(refs[i])); err != nil {
			t.Fatal(err)
		}
		// An equal number of unreachable objects exercises captured promotion.
		if _, err = c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	for i, ref := range refs {
		if err = c.StructSet(ref, 3, Value{Kind: StorageExternRef, Bits: 0x1234567800000002}); err != nil {
			t.Fatal(err)
		}
		if err = c.StructSet(ref, 0, RefValue(refs[(i+1)%count])); err != nil {
			t.Fatal(err)
		}
		if err = c.StructSet(ref, 2, RefValue(ref)); err != nil {
			t.Fatal(err)
		}
	}
	return c, parent, refs
}

func TestParallelCollectionGraphEquivalence(t *testing.T) {
	parallelTestProcessors(t)
	for _, workers := range []int{1, 2, 4, 8} {
		for _, immediate := range []bool{false, true} {
			t.Run(fmt.Sprintf("workers=%d/immediate=%v", workers, immediate), func(t *testing.T) {
				c, parent, refs := newParallelTestGraph(t, workers, immediate)
				roots := RefSliceRoots{parent, parent, refs[0], Null(), I31New(17)}
				want, err := shadowTraceLive(c, roots)
				if err != nil {
					t.Fatal(err)
				}
				if err = c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				for h := 1; h < len(c.handles); h++ {
					if (c.handles[h].space != spaceFree) != want[h] {
						t.Fatalf("full liveness differs at %d", h)
					}
				}
				if workers > 1 && c.parallel == nil {
					t.Fatal("parallel path not selected")
				}
				if workers == 1 && c.parallel != nil {
					t.Fatal("single worker created helpers")
				}
				for cycle := 0; cycle < 3; cycle++ {
					before := c.stats.MinorObjectsScanned
					young := len(c.nurseryHandles)
					if err = c.CollectMinor(roots); err != nil {
						t.Fatal(err)
					}
					if got := c.stats.MinorObjectsScanned - before; got != uint64(young) {
						t.Fatalf("scans=%d want=%d", got, young)
					}
					if c.Stats().LiveObjects != uint32(len(refs)+1) {
						t.Fatal("minor lost reachable graph")
					}
					if err = c.CollectFull(roots); err != nil {
						t.Fatal(err)
					}
				}
				if err = c.CollectFull(nil); err != nil {
					t.Fatal(err)
				}
				if c.Stats().LiveObjects != 0 {
					t.Fatal("unreachable cycle retained")
				}
				c.Close()
				if c.parallel != nil {
					t.Fatal("pool retained after close")
				}
				c.Close()
			})
		}
	}
}

func TestParallelMinorRememberedAndPersistentRoots(t *testing.T) {
	parallelTestProcessors(t)
	c, parent, refs := newParallelTestGraph(t, 4, false)
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	// All young edges now enter through the old array's remembered cards.
	slot := c.NewGlobalSlot(parent)
	table := c.NewTableSlot(refs[0])
	for cycle := 0; cycle < 3; cycle++ {
		if err := c.CollectMinor(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if c.parallel == nil {
			t.Fatal("minor did not use helpers")
		}
		if c.Stats().LiveObjects != uint32(len(refs)+1) {
			t.Fatal("remembered/persistent roots lost graph")
		}
	}
	if err := c.SetGlobalSlot(slot, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.SetTableSlot(table, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 {
		t.Fatal("persistent roots retained graph after clearing")
	}
}

func TestParallelSerialFallbackAndConfig(t *testing.T) {
	for _, cfg := range []Config{{Workers: -1}, {Workers: 9}, {Workers: 2, Profile: ProfileTiny}} {
		if err := ValidateConfig(cfg); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
	for _, cfg := range []Config{{}, {Workers: 1}, {Workers: 1, Profile: ProfileTiny}, {Workers: 8}} {
		if err := ValidateConfig(cfg); err != nil {
			t.Fatal(err)
		}
	}
	parallelTestProcessors(t)
	c := newTestCollector(t, Config{Workers: 4})
	defer c.Close()
	r, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.CollectFull(RefSliceRoots{r}); err != nil {
		t.Fatal(err)
	}
	if c.parallel != nil {
		t.Fatal("small collection started helpers")
	}
	c2, parent, _ := newParallelTestGraph(t, 4, false)
	runtime.GOMAXPROCS(1)
	if err = c2.CollectFull(RefSliceRoots{parent}); err != nil {
		t.Fatal(err)
	}
	if c2.parallel != nil {
		t.Fatal("one processor started helpers")
	}
}

func TestParallelEpochGrowthAndPrivateChain(t *testing.T) {
	parallelTestProcessors(t)
	c, _, refs := newParallelTestGraph(t, 4, false)
	// The single cycle can be traced entirely on one private stack. Idle helpers
	// must still observe termination without an edge-by-edge wake or counter.
	roots := RefSliceRoots{refs[0]}
	for range 4 {
		if err := c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
	}
	c.parallel.epoch = ^uint32(0)
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.parallel.epoch != 1 {
		t.Fatal("epoch wrap not reset")
	}
	for range 10000 {
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if len(c.parallel.claims) < len(c.handles) {
		t.Fatal("claim table did not grow")
	}
	if c.Stats().LiveObjects != uint32(len(refs)) {
		t.Fatal("chain liveness changed")
	}
}

func TestParallelLargeArrayChunks(t *testing.T) {
	parallelTestProcessors(t)
	leaf, _ := NewStructDesc(0, nil)
	array, _ := NewArrayDesc(1, StorageRefNull)
	c, err := NewCollector(Config{Workers: 4, ThroughputHeapBytes: 32 << 20}, []TypeDesc{leaf, array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	// More ranges than the shared queue holds: overflow stays in private work.
	parent, err := c.NewArray(1, parallelArrayChunk*(parallelQueueLimit+17), RefValue(child))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err = c.CollectFull(RefSliceRoots{parent}); err != nil {
			t.Fatal(err)
		}
		if c.parallel == nil {
			t.Fatal("large array with few handles ran serially")
		}
		participants := 0
		var tasks uint64
		for _, w := range c.parallel.workers {
			tasks += w.tasks
			if w.tasks != 0 {
				participants++
			}
		}
		if participants < 2 {
			t.Fatalf("only %d workers scanned array chunks", participants)
		}
		if tasks != parallelQueueLimit+18 {
			t.Fatalf("tasks=%d; array ranges or child duplicated/lost", tasks)
		}
		if c.Stats().LiveObjects != 2 {
			t.Fatal("array lost child")
		}
	}
}
