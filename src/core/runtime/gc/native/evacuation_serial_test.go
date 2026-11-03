package gc

import (
	"reflect"
	"testing"
)

func stage7Heap(t *testing.T, workers, count int, shift uint8, enabled bool) (*Collector, RefSliceRoots) {
	t.Helper()
	c := newTestCollector(t, Config{Workers: workers, NurseryBytes: 1 << 20, NurseryRegionShift: shift, MatureEvacuation: enabled, ThroughputHeapBytes: 32 << 20, ThroughputPageBytes: 4096, PoisonFreed: true, VerifyAfterCollect: true})
	refs := make(RefSliceRoots, 0, count)
	var gaps []handleEntry
	for i := 0; i < count; i++ {
		r, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.promoteHandle(handleOf(r)); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, r)
		if (i+1)%128 == 0 {
			gap, err := c.throughput.alloc(32768-4096, spaceOld)
			if err != nil {
				t.Fatal(err)
			}
			gaps = append(gaps, gap)
		}
	}
	for _, e := range gaps {
		if err := c.throughput.free(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Verify(refs); err != nil {
		t.Fatal(err)
	}
	return c, refs
}

func TestEvacuationStage7RunCredit(t *testing.T) {
	free := []throughputFreeSpan{{0, 32768}, {32768, 32768}, {65536, 32768}}
	for _, workers := range []int{1, 2, 4, 8} {
		runs, credit, need, err := planEvacuationRuns(nil, nil, 0, 0, workers, 65536)
		if err != nil || len(runs) != 0 || credit != 0 || need != 0 {
			t.Fatal("zero demand needs slack")
		}
	}
	runs, credit, need, err := planEvacuationRuns(free, nil, 32704, 32, 1, 65536)
	if err != nil || len(runs) != 2 || credit != 65472 || need != 65472 {
		t.Fatal(runs, credit, need, err)
	}
	if _, _, _, err = planEvacuationRuns(free[:2], nil, 32705, 32, 1, 65536); err == nil {
		t.Fatal("one-byte deficit")
	}
	if _, _, _, err = planEvacuationRuns(free, []bool{false, true, true}, 32, 32, 1, 65536); err == nil {
		t.Fatal("source exclusion ignored")
	}
	if _, _, _, err = planEvacuationRuns(free, nil, 32, 32, 1, 1); err == nil {
		t.Fatal("run cap ignored")
	}
	if _, _, _, err = planEvacuationRuns(free, nil, ^uint64(0), 32, 8, 65536); err == nil {
		t.Fatal("overflow")
	}
}

func TestEvacuationStage7PolicyAndOptionalRefusal(t *testing.T) {
	for _, cfg := range []Config{{MatureEvacuation: true}, {MatureEvacuation: true, NurseryRegionShift: 6, NurseryBytes: 4096, DisableCollection: true}, {MatureEvacuation: true, NurseryRegionShift: 6, NurseryBytes: 4096, Profile: ProfileTiny}} {
		if ValidateConfig(cfg) == nil {
			t.Fatal("accepted invalid policy")
		}
	}
	c, roots := stage7Heap(t, 1, 1, 6, true)
	before := *c.entry(roots[0])
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if *c.entry(roots[0]) != before || c.CollectionDiagnostics().Evacuation != "no-pressure" {
		t.Fatal("moved without pressure")
	}
	c.throughput.limit = 32768
	c.throughput.mature.pressure = true
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	d := c.CollectionDiagnostics()
	if d.Evacuation != "credit" || d.Pressure || d.Result != "success" || *c.entry(roots[0]) != before {
		t.Fatal(d)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.CollectionDiagnostics().Evacuation != "no-pressure" {
		t.Fatal("pressure not consumed")
	}
	c.throughput.mature.pressure = true
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	c.throughput.mature.pressure = true
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if d = c.CollectionDiagnostics(); d.Evacuation != "no-candidates" || d.Pressure {
		t.Fatal(d)
	}
}

func TestEvacuationStage7Selection(t *testing.T) {
	c := newTestCollector(t, Config{NurseryRegionShift: 6, NurseryBytes: 4096, MatureEvacuation: true, ThroughputHeapBytes: 8 * 32768})
	h := &c.throughput
	h.bump = 8 * 32768
	extent := func(off, size uint32, space spaceKind) handleEntry {
		return handleEntry{off: off, size: size, space: space, class: uint16(len(throughputClassSizes))}
	}
	c.handles = []handleEntry{{}, extent(0, 256, spaceOld), extent(32768, 32*256, spaceOld), extent(2*32768, 33*256, spaceOld), extent(4*32768-256, 512, spaceOld), extent(5*32768, 32, spaceOld), extent(5*32768+32, 32768, spaceLarge)}
	h.mature.pressure = true
	s, err := c.selectMatureEvacuation()
	if err != nil {
		t.Fatal(err)
	}
	got := []uint32{}
	for _, m := range s.moves {
		got = append(got, m.handle)
	}
	if !reflect.DeepEqual(got, []uint32{1, 2, 4}) {
		t.Fatal("line/crossing/LOS selection", got)
	}
	if !s.excluded[3] || !s.excluded[4] || s.excluded[5] || s.excluded[6] {
		t.Fatal(s.excluded)
	}
	// A dense second half excludes an otherwise sparse crossing component.
	c.handles = append(c.handles, extent(4*32768+256, 32*256, spaceOld))
	s, err = c.selectMatureEvacuation()
	if err != nil {
		t.Fatal(err)
	}
	if s.excluded[3] || s.excluded[4] {
		t.Fatal("split component selected")
	}
	// Tiny budgets rotate the selected root without truncating a component.
	c.handles = []handleEntry{{}, extent(0, 32, spaceOld), extent(32768, 32, spaceOld), extent(65536, 32, spaceOld)}
	seen := map[uint32]bool{}
	for range 8 {
		s, err = c.selectMatureEvacuationBounded(1)
		if err != nil || len(s.moves) != 1 {
			t.Fatal(s, err)
		}
		seen[s.moves[0].handle] = true
	}
	if len(seen) != 3 {
		t.Fatal("candidate rotation starved", seen)
	}
}

func TestEvacuationStage7CrossingPhysicalMove(t *testing.T) {
	c := newTestCollector(t, Config{NurseryRegionShift: 12, NurseryBytes: 4096, MatureEvacuation: true, ThroughputHeapBytes: 1 << 20, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	gap, err := c.throughput.alloc(32768-256, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.NewArrayDefault(2, 124)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ArraySet(r, 123, I32Value(1234567)); err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(r); err != nil {
		t.Fatal(err)
	}
	if c.entry(r).off != 32512 || c.entry(r).size != 512 {
		t.Fatal("not crossing", *c.entry(r))
	}
	if err = c.throughput.free(gap); err != nil {
		t.Fatal(err)
	}
	c.throughput.mature.pressure = true
	if err = c.CollectFull(RefSliceRoots{r}); err != nil {
		t.Fatal(err)
	}
	if c.entry(r).off < 65536 || c.CollectionDiagnostics().MovedPhysical != 512 {
		t.Fatal("crossing object stayed or moved into source block")
	}
	v, err := c.ArrayGet(r, 123)
	if err != nil || v.Bits != 1234567 {
		t.Fatal(v, err)
	}
	stage4Oracle(t, c, map[Ref]uint32{r: 512})
}

func TestEvacuationStage7AbsorbedTail(t *testing.T) {
	c := newTestCollector(t, Config{NurseryRegionShift: 6, NurseryBytes: 4096, MatureEvacuation: true, ThroughputHeapBytes: 1 << 20, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	hole, err := c.throughput.alloc(48, spaceLarge)
	if err != nil {
		t.Fatal(err)
	}
	barrier, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(barrier); err != nil {
		t.Fatal(err)
	}
	if err = c.throughput.free(hole); err != nil {
		t.Fatal(err)
	}
	r, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(r); err != nil {
		t.Fatal(err)
	}
	before := *c.entry(r)
	if before.off != 0 || before.class&handleAbsorbedTailBit == 0 {
		t.Fatal("tail fixture", before)
	}
	c.throughput.mature.pressure = true
	if err = c.CollectFull(RefSliceRoots{r, barrier}); err != nil {
		t.Fatal(err)
	}
	if c.entry(r).off == before.off || c.entry(r).class != before.class || c.CollectionDiagnostics().MovedPhysical != 80 {
		t.Fatal("physical tail lost", c.CollectionDiagnostics())
	}
	if err = c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.throughput.bump != 0 || c.throughput.pendingBytes != 0 {
		t.Fatal("tail leaked")
	}
}

func TestEvacuationStage7LargeYoungPlan(t *testing.T) {
	c := newTestCollector(t, Config{NurseryRegionShift: 6, NurseryBytes: 4 << 20, MatureEvacuation: true, ThroughputHeapBytes: 16 << 20, VerifyAfterCollect: true})
	roots := make(RefSliceRoots, 65537)
	for i := range roots {
		r, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		roots[i] = r
	}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	if c.Stats().PromotedBytes != uint64(len(roots))*32 || len(c.nurseryHandles) != 0 {
		t.Fatal("candidate cap incorrectly limited young plan")
	}
	if d := c.CollectionDiagnostics(); d.Runs <= 0 || d.Runs > 65536 {
		t.Fatal(d)
	}
}
