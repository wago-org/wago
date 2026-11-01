//go:build !tinygo && !wago_target_tinygo

package gc

import "testing"

func TestEvacuationStage7ExactAdmissionEdges(t *testing.T) {
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
func TestEvacuationStage7CheckedGeneration(t *testing.T) {
	c, roots := stage7Heap(t, 1, 128, 6, true)
	if err := c.EnableCheckedHandles(); err != nil {
		t.Fatal(err)
	}
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
func TestEvacuationStage7FullHeapZeroDemand(t *testing.T) {
	for _, workers := range []int{1, 2, 4, 8} {
		c := newTestCollector(t, Config{Workers: workers, NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 16384, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
		large, err := c.NewArrayDefault(2, 4092)
		if err != nil {
			t.Fatal(err)
		}
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if c.throughput.bump != c.throughput.limit {
			t.Fatal("old/LOS heap not full")
		}
		before := *c.entry(large)
		roots := RefSliceRoots{large, child}
		if err = c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		d := c.CollectionDiagnostics()
		if d.Demand != 0 || d.Need != 0 || d.Runs != 0 || c.entry(large).off != before.off {
			t.Fatal("zero destination demand refused", d)
		}
		c.throughput.mature.pressure = true
		if err = c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
		d = c.CollectionDiagnostics()
		if d.Evacuation != "no-candidates" || d.Pressure {
			t.Fatal(d)
		}
	}
}
