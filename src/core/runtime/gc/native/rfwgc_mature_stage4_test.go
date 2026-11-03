package gc

import (
	"fmt"
	"sort"
	"testing"
)

func stage4Collector(t *testing.T, shift uint8) *Collector {
	t.Helper()
	c, err := NewCollector(Config{NurseryBytes: 4096, NurseryRegionShift: shift, DisableMovingNursery: true, ThroughputHeapBytes: 16384, ThroughputPageBytes: 4096, VerifyAfterCollect: true, PoisonFreed: true}, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if (len(c.edenRegions) != 0) != (shift != 0) {
		t.Fatal("wrong Eden policy")
	}
	return c
}

// The fixture supplies the reachable set and physical sizes independently of
// GC marks, counters, and handleAllocSize. These fixtures use 32-byte struct
// physical slots and exact aligned array extents; absorbed-tail cases need a separate oracle.
func stage4Oracle(t *testing.T, c *Collector, live map[Ref]uint32) {
	t.Helper()
	type interval struct{ start, end uint32 }
	var spans []interval
	count := 0
	for i, e := range c.handles {
		if e.space == spaceFree {
			continue
		}
		count++
		r := Ref(uint32(i) << 1)
		size, ok := live[r]
		if !ok {
			t.Fatalf("unexpected live handle %d", i)
		}
		if e.space != spaceOld && e.space != spaceLarge {
			t.Fatalf("not tenured: %+v", e)
		}
		if e.size != size {
			t.Fatalf("layout size %d != oracle %d", e.size, size)
		}
		spans = append(spans, interval{e.off, e.off + (size+15)&^15})
	}
	if count != len(live) || c.Stats().LiveObjects != uint32(len(live)) {
		t.Fatalf("live count %d stats %d want %d", count, c.Stats().LiveObjects, len(live))
	}
	h := &c.throughput
	var walk func(uint32)
	seen := map[uint32]bool{}
	walk = func(n uint32) {
		if n == throughputNoSlot {
			return
		}
		if int(n) >= len(h.spanNodes) || seen[n] {
			t.Fatal("invalid free tree")
		}
		seen[n] = true
		s := h.spanNodes[n]
		spans = append(spans, interval{s.off, s.off + s.size})
		walk(s.left)
		walk(s.right)
	}
	walk(h.spanRoot)
	for _, s := range h.pendingFree {
		spans = append(spans, interval{s.off, s.off + s.size})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var end uint32
	for _, s := range spans {
		if s.start != end || s.end <= s.start || s.end > h.bump {
			t.Fatalf("extent gap/overlap: previous=%d span=%+v bump=%d", end, s, h.bump)
		}
		end = s.end
	}
	if end != h.bump || h.bump > h.limit || len(h.mem) > int(h.limit) {
		t.Fatalf("unaccounted or unbounded backing: end=%d bump=%d len=%d", end, h.bump, len(h.mem))
	}
}

func TestRFWGCMatureStage4PromotionChurn(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c := stage4Collector(t, shift)
			parent := regionStruct(t, c, 1, EmptyRoots{})
			roots := Slots{(*Root)(&parent)}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			for cycle := 0; cycle < 20001; cycle++ {
				child := regionStruct(t, c, 0, roots)
				if shift != 0 && c.edenRegions[c.entry(child).off>>shift].bump == 0 {
					t.Fatal("no region occupancy")
				}
				if err := c.StructSet(child, 1, I64Value(int64(cycle))); err != nil {
					t.Fatal(err)
				}
				if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
					t.Fatal(err)
				}
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				v, err := c.StructGet(child, 1)
				if err != nil || v.I64() != int64(cycle) {
					t.Fatal("payload oracle", v, err)
				}
				edge, err := c.StructGet(parent, 0)
				if err != nil || edge.Ref != child {
					t.Fatal("edge oracle", edge, err)
				}
				stage4Oracle(t, c, map[Ref]uint32{parent: 24, child: 32})
				if c.throughput.bump > 96 || len(c.handles) > 4 {
					t.Fatalf("unbounded churn: bump=%d handles=%d", c.throughput.bump, len(c.handles))
				}
			}
			if c.Stats().PromotedBytes < 20001*32+24 {
				t.Fatal("did not promote every cycle")
			}
			if err := c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			stage4Oracle(t, c, nil)
		})
	}
}

func TestRFWGCMatureStage4LargeReclamation(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c := stage4Collector(t, shift)
			var refs [2]Ref
			roots := Slots{}
			for i := range refs {
				roots = append(roots, (*Root)(&refs[i]))
			}
			offsets := map[uint32]bool{}
			for i := range refs {
				r, err := c.NewArrayDefaultWithRoots(2, 2044, roots)
				if err != nil {
					t.Fatal(err)
				}
				refs[i] = r
				if c.entry(r).space != spaceLarge || c.entry(r).size != 8192 {
					t.Fatalf("wrong large extent %+v", c.entry(r))
				}
			}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			anchor := refs[0]
			anchorEntry := *c.entry(anchor)
			for cycle := 0; cycle < 128; cycle++ {
				offsets[c.entry(refs[1]).off] = true
				refs[1] = 0
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				stage4Oracle(t, c, map[Ref]uint32{anchor: 8192})
				r, err := c.NewArrayDefaultWithRoots(2, 2044, roots)
				if err != nil {
					t.Fatal(err)
				}
				refs[1] = r
				if err := c.ArraySet(r, 2043, I32Value(int32(cycle))); err != nil {
					t.Fatal(err)
				}
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				v, err := c.ArrayGet(r, 2043)
				if err != nil || v.I32() != int32(cycle) {
					t.Fatal("large tail", v, err)
				}
				if *c.entry(anchor) != anchorEntry {
					t.Fatal("large anchor moved")
				}
				stage4Oracle(t, c, map[Ref]uint32{anchor: 8192, r: 8192})
			}
			if len(offsets) != 1 {
				t.Fatal("large extent not reused")
			}
			if err := c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			stage4Oracle(t, c, nil)
		})
	}
}

func TestRFWGCMatureStage4FragmentedPromotionAndZeroBytes(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c := stage4Collector(t, shift)
			var refs [16]Ref
			roots := Slots{}
			for i := range refs {
				roots = append(roots, (*Root)(&refs[i]))
				refs[i] = regionStruct(t, c, 0, roots)
			}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			holes := map[uint32]bool{}
			live := map[Ref]uint32{}
			for i, r := range refs {
				if i%2 == 0 {
					holes[c.entry(r).off] = true
					refs[i] = 0
				} else {
					live[r] = 32
				}
			}
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			stage4Oracle(t, c, live)
			bump := c.throughput.bump
			promoted := c.Stats().PromotedBytes
			for i := 0; i < 64; i++ {
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
			}
			if c.throughput.bump != bump || c.Stats().PromotedBytes != promoted {
				t.Fatal("zero-promoting cycle consumed destination bytes")
			}
			for i := range refs {
				if i%2 == 0 {
					refs[i] = regionStruct(t, c, 0, roots)
				}
			}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			for i, r := range refs {
				live[r] = 32
				if i%2 == 0 && !holes[c.entry(r).off] {
					t.Fatal("fragment not reused")
				}
			}
			if c.throughput.bump != bump {
				t.Fatal("fragmented promotion grew backing")
			}
			stage4Oracle(t, c, live)
		})
	}
}

func TestRFWGCMatureStage4AdmissionRetry(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c := stage4Collector(t, shift)
			var a, b, young Ref
			roots := Slots{(*Root)(&a), (*Root)(&b), (*Root)(&young)}
			var err error
			a, err = c.NewArrayDefaultWithRoots(2, 2044, roots)
			if err != nil {
				t.Fatal(err)
			}
			b, err = c.NewArrayDefaultWithRoots(2, 2044, roots)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			young = regionStruct(t, c, 0, roots)
			if err = c.StructSet(young, 1, I64Value(123456)); err != nil {
				t.Fatal(err)
			}
			before := *c.entry(young)
			bump := c.throughput.bump
			promoted := c.Stats().PromotedBytes
			if err = c.CollectMinor(roots); err == nil {
				t.Fatal("full heap admitted promotion")
			}
			if *c.entry(young) != before || c.throughput.bump != bump || c.Stats().PromotedBytes != promoted {
				t.Fatal("failed admission mutated committed state")
			}
			if err = c.Verify(roots); err != nil {
				t.Fatal(err)
			}
			b = 0
			if err = c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if err = c.CollectMinor(roots); err != nil {
				t.Fatal("retry", err)
			}
			v, err := c.StructGet(young, 1)
			if err != nil || v.I64() != 123456 {
				t.Fatal("retry payload", v, err)
			}
			stage4Oracle(t, c, map[Ref]uint32{a: 8192, young: 32})
		})
	}
}

// Live copying without promotion must not require old capacity. This uses
// survivor semispaces, unlike the immediate-promotion stress fixtures above.
func TestRFWGCMatureStage4SurvivorWithoutOldCredit(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c, err := NewCollector(Config{NurseryBytes: 4096, NurseryRegionShift: shift, SurvivorBytes: 4096, ThroughputHeapBytes: 16384, ThroughputPageBytes: 4096, VerifyAfterCollect: true}, testTypes(t))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var a, b, child Ref
			roots := Slots{(*Root)(&a), (*Root)(&b), (*Root)(&child)}
			a, err = c.NewArrayDefaultWithRoots(2, 2044, roots)
			if err != nil {
				t.Fatal(err)
			}
			b, err = c.NewArrayDefaultWithRoots(2, 2044, roots)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.ForcePromote(a); err != nil {
				t.Fatal(err)
			}
			if err = c.ForcePromote(b); err != nil {
				t.Fatal(err)
			}
			child = regionStruct(t, c, 0, roots)
			before := c.Stats().PromotedBytes
			if err = c.CollectMinor(roots); err != nil {
				t.Fatal("survivor-only collection required old capacity", err)
			}
			if c.entry(child).space != spaceNursery || c.entry(child).off < c.edenBytes() || c.Stats().PromotedBytes != before || c.throughput.bump != 16384 {
				t.Fatal("not a zero-promotion survivor cycle")
			}
			if err = c.Verify(roots); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Covers a late LOS slot above 64 KiB in Wago's shared backing. Generated range
// barriers and source-size (256 MiB live graph) qualification remain separate.
func TestRFWGCMatureStage4HighLargeCard(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c, err := NewCollector(Config{NurseryBytes: 4096, NurseryRegionShift: shift, DisableMovingNursery: true, ThroughputHeapBytes: 262144, ThroughputPageBytes: 4096, VerifyAfterCollect: true, PoisonFreed: true}, testTypes(t))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var pad, array Ref
			roots := Slots{(*Root)(&pad), (*Root)(&array)}
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
			if c.entry(array).off < 65536 || c.entry(array).space != spaceLarge {
				t.Fatal("missing high LOS extent")
			}
			original := *c.entry(array)
			for cycle := 0; cycle < 128; cycle++ {
				child := regionStruct(t, c, 0, roots)
				if err = c.StructSet(child, 1, I64Value(int64(42+cycle))); err != nil {
					t.Fatal(err)
				}
				if err = c.ArraySet(array, 9999, RefValue(child)); err != nil {
					t.Fatal(err)
				}
				if err = c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				if err = c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				edge, err := c.ArrayGet(array, 9999)
				if err != nil || edge.Ref != child {
					t.Fatal("high card lost edge", edge, err)
				}
				v, err := c.StructGet(child, 1)
				if err != nil || v.I64() != int64(42+cycle) {
					t.Fatal("high card lost payload", v, err)
				}
				if *c.entry(array) != original {
					t.Fatal("LOS extent moved")
				}
				stage4Oracle(t, c, map[Ref]uint32{pad: 65536, array: 40016, child: 32})
			}
			if err = c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			stage4Oracle(t, c, nil)
		})
	}
}
