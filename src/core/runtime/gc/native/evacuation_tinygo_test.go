//go:build tinygo || wago_target_tinygo

package gc

import (
	"reflect"
	"testing"
)

func TestEvacuationTinyGoExactAdmissionEdges(t *testing.T) {
	free := make([]throughputFreeSpan, 1025)
	for i := range free {
		free[i] = throughputFreeSpan{uint32(i * 64), 32}
	}
	if _, credit, need, err := planEvacuationRuns(free, nil, 32, 32, 1, 65536); err == nil || credit != 0 || need != 32800 {
		t.Fatal("fragmented aggregate is not usable credit", credit, need, err)
	}
	runs, credit, need, err := planEvacuationRuns([]throughputFreeSpan{{0, 32768}, {32768, 96}}, nil, 32, 32, 1, 2)
	if err != nil || len(runs) != 2 || cap(runs) > 2 || runs[1].size != 96 || credit != 32800 || need != 32800 {
		t.Fatal("partial final block", runs, credit, need, err)
	}
	if _, _, _, err = planEvacuationRuns([]throughputFreeSpan{{0, 32768}, {32768, 80}}, nil, 32, 32, 1, 2); err == nil {
		t.Fatal("partial tail received synthetic credit")
	}
	if _, _, _, err = planEvacuationRuns([]throughputFreeSpan{{0, 32768}, {16, 32768}}, nil, 32, 32, 1, 2); err == nil {
		t.Fatal("overlapping credit counted twice")
	}
	if _, _, _, err = planEvacuationRuns([]throughputFreeSpan{{0, 65536}}, nil, 32769, 32769, 1, 2); err == nil {
		t.Fatal("oversized proof")
	}
}
func TestEvacuationTinyGoCheckedGeneration(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 1 << 20, VerifyAfterCollect: true})
	if err := c.EnableCheckedHandles(); err != nil {
		t.Fatal(err)
	}
	r, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(r); err != nil {
		t.Fatal(err)
	}
	roots := RefSliceRoots{r}
	generation, err := c.CheckedIdentity(roots[0])
	if err != nil {
		t.Fatal(err)
	}
	before := *c.entry(roots[0])
	c.throughput.mature.pressure = true
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	after, err := c.CheckedIdentity(roots[0])
	if err != nil || generation != after || c.entry(roots[0]).off == before.off {
		t.Fatal("checked identity changed or no movement", generation, after, err)
	}
}
func TestEvacuationTinyGoCardsLOSAndYoung(t *testing.T) {
	c := newTestCollector(t, Config{Workers: 1, NurseryRegionShift: 12, NurseryBytes: 1 << 20, SurvivorBytes: 1 << 20, MatureEvacuation: true, ThroughputHeapBytes: 8 << 20, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	// Place a nonmoving high-card LOS array in a different overlay block.
	gap, err := c.throughput.alloc(32768-32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	large, err := c.NewArrayDefault(3, 20000)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(large); err != nil {
		t.Fatal(err)
	}
	if err = c.throughput.free(gap); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.StructSet(child, 0, I32Value(197)); err != nil {
		t.Fatal(err)
	}
	if err = c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	for _, i := range []uint32{9999, 19999} {
		if err = c.ArraySet(large, i, RefValue(child)); err != nil {
			t.Fatal(err)
		}
	}
	// Self/cycle edges retain stable words through old relocation.
	if err = c.StructSet(parent, 1, RefValue(parent)); err != nil {
		t.Fatal(err)
	}
	beforeParent, beforeLarge, beforeChild := *c.entry(parent), *c.entry(large), *c.entry(child)
	cards := append([]objectCard(nil), c.objectCards...)
	nurseryBump := c.nurseryBump
	youngCopied := c.Stats().YoungBytesCopied
	c.throughput.mature.pressure = true
	if err = c.CollectFull(RefSliceRoots{parent, large}); err != nil {
		t.Fatal(err)
	}
	if c.entry(parent).off == beforeParent.off || *c.entry(large) != beforeLarge || *c.entry(child) != beforeChild || c.nurseryBump != nurseryBump || c.Stats().YoungBytesCopied != youngCopied {
		t.Fatal("old/LOS/young motion contract", c.CollectionDiagnostics())
	}
	if c.entry(parent).cardSlot != beforeParent.cardSlot || !reflect.DeepEqual(cards, c.objectCards) || c.lastCardBounds != (objectCardBounds{}) {
		t.Fatal("card identity")
	}
	if err = c.CollectMinor(RefSliceRoots{parent, large}); err != nil {
		t.Fatal(err)
	}
	if v, err := c.StructGet(child, 0); err != nil || v.Bits != 197 {
		t.Fatal("young child lost", v, err)
	}
	for _, i := range []uint32{9999, 19999} {
		v, err := c.ArrayGet(large, i)
		if err != nil || v.Ref != child {
			t.Fatal(v, err)
		}
	}
}

func TestEvacuationTinyGoRequiredPressure(t *testing.T) {
	for _, workers := range []int{0, 1} {
		for _, shift := range []uint8{0, 6, 12} {
			for _, enabled := range []bool{false, true} {
				if enabled && shift == 0 {
					continue
				}
				c, roots := stage7Heap(t, workers, 129, shift, enabled)
				before := *c.entry(roots[0])
				if err := c.StructSet(roots[0], 0, I32Value(12345)); err != nil {
					t.Fatal(err)
				}
				if _, err := c.NewArrayDefault(2, 9000000); err == nil {
					t.Fatal("required admission unexpectedly succeeded")
				}
				if shift != 0 && !c.CollectionDiagnostics().Pressure {
					t.Fatal("required refusal did not latch pressure")
				}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				d := c.CollectionDiagnostics()
				if enabled {
					if d.Evacuation != "moved" || d.CopyWorkers != 1 || d.ReservationWorkers != 1 || d.Pressure || c.entry(roots[0]).off == before.off {
						t.Fatal("serial movement", d)
					}
				} else if *c.entry(roots[0]) != before {
					t.Fatal("opt-out moved")
				}
				if v, err := c.StructGet(roots[0], 0); err != nil || v.Bits != 12345 {
					t.Fatal(v, err)
				}
				if err := c.CollectFull(nil); err != nil {
					t.Fatal(err)
				}
				if c.Stats().LiveObjects != 0 {
					t.Fatal("retained graph")
				}
				c.Close()
				c.Close()
			}
		}
	}
}
