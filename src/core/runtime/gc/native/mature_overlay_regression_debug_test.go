//go:build wagodebug

package gc

import (
	"reflect"
	"slices"
	"testing"
)

func TestMatureBoundedOverlayTailRollback(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1 << 20, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 1 << 30, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	old, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(old); err != nil {
		t.Fatal(err)
	}
	h := &c.throughput
	before := slices.Clone(c.handles)
	bump := h.bump
	backing := len(h.mem)
	free := h.freeSpans()
	h.mature.pressure = true
	disarm := armFailure(c, failEvacuationCommit, 0)
	err = c.CollectFull(RefSliceRoots{old})
	disarm()
	if !isInjectedFailure(err) {
		t.Fatal("commit injection not reached", err)
	}
	if !reflect.DeepEqual(before, c.handles) || h.bump != bump || len(h.mem) != backing || !reflect.DeepEqual(free, h.freeSpans()) {
		t.Fatal("rollback changed ownership/backing")
	}
	if err = c.Verify(RefSliceRoots{old}); err != nil {
		t.Fatal(err)
	}
	h.mature.pressure = true
	if err = c.CollectFull(RefSliceRoots{old}); err != nil {
		t.Fatal(err)
	}
	if c.CollectionDiagnostics().Evacuation != "moved" || c.entry(old).off < bump {
		t.Fatal("retry did not use tail")
	}
	if err = c.Verify(RefSliceRoots{old}); err != nil {
		t.Fatal(err)
	}
}
