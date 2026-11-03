package gc

import "testing"

// Public pressure collection must grow beyond the active source overlay and
// preserve a nonmoving LOS plus current handle ownership over repeated moves.
func TestMatureBoundedOverlayPublicTail(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1 << 20, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 1 << 30, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	old, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(old); err != nil {
		t.Fatal(err)
	}
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
	los := *c.entry(large)
	roots := RefSliceRoots{old, large}
	for i := 0; i < 3; i++ {
		before := *c.entry(old)
		bump := c.throughput.bump
		c.throughput.mature.pressure = true
		if err = c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
		if d := c.CollectionDiagnostics(); i == 0 && d.Evacuation != "moved" {
			t.Fatal(d)
		}
		if (i == 0 && c.entry(old).off == before.off) || *c.entry(large) != los {
			t.Fatal("ownership/LOS")
		}
		if i == 0 && c.entry(old).off < bump {
			t.Fatal("first move did not use uncommitted tail")
		}
		if err = c.Verify(roots); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMatureBoundedActualLimits(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 32 << 20})
	h := &c.throughput
	// 128 sparse objects/block, 513 blocks: 65536 admitted plus one refused component.
	h.bump = 513 * uint32(evacuationBlock)
	h.mem = make([]byte, h.bump)
	h.mature.pressure = true
	c.handles = make([]handleEntry, 1, 65538)
	for i := 0; i < 65537; i++ {
		c.handles = append(c.handles, handleEntry{off: uint32(i/128)*uint32(evacuationBlock) + uint32(i%128)*32, size: 32, space: spaceOld, class: uint16(len(throughputClassSizes))})
	}
	s, err := c.selectMatureEvacuation()
	if err != nil || len(s.moves) != 65536 || c.CollectionDiagnostics().Evacuation != "budget" {
		t.Fatal(len(s.moves), err, c.CollectionDiagnostics())
	}
	free := make([]throughputFreeSpan, 65537)
	for i := range free {
		free[i] = throughputFreeSpan{off: uint32(i) * 64, size: 32}
	}
	if runs, _, _, err := planEvacuationRuns(free, nil, 65536*16-32768, 16, 1, evacuationRunLimit); err != nil || len(runs) != 65536 {
		t.Fatal(len(runs), err)
	}
	// Each run earns 16 bytes; require one more than the exact 65536 cap.
	if _, _, _, err := planEvacuationRuns(free, nil, 65536*16+1-32768, 16, 1, evacuationRunLimit); err == nil || err.Error() != "gc: run metadata limit" {
		t.Fatal(err)
	}
}

// Valid earlier scratch writes must not publish ownership if a later physical
// source is invalid. Suitable regression when validation rejoins occupancy.
func TestMatureBoundedLaterMalformedSource(t *testing.T) {
	for _, sp := range []spaceKind{spaceOld, spaceLarge} {
		for _, tail := range []bool{false, true} {
			c := newTestCollector(t, Config{NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 1 << 30})
			h := &c.throughput
			h.bump = 64
			h.mem = make([]byte, 4096)
			h.mature.pressure = true
			h.mature.cursor = 17
			cls := uint16(len(throughputClassSizes))
			off := uint32(64)
			if tail {
				cls |= handleAbsorbedTailBit
				off = 32
			}
			c.handles = []handleEntry{{}, {off: 0, size: 32, space: spaceOld, class: uint16(len(throughputClassSizes))}, {off: off, size: 32, space: sp, class: cls}}
			before := append([]handleEntry(nil), c.handles...)
			if _, err := c.selectMatureEvacuation(); err == nil {
				t.Fatal("accepted malformed later source", sp, tail)
			}
			if h.bump != 64 || len(h.mem) != 4096 || h.mature.cursor != 17 {
				t.Fatal("changed allocator/cursor")
			}
			for i := range before {
				if c.handles[i] != before[i] {
					t.Fatal("changed handle")
				}
			}
		}
	}
}
