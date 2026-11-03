//go:build linux && !tinygo && !wago_target_tinygo

package gc

import "testing"

func TestEvacuationStage7ShadowEdges(t *testing.T) {
	parallelTestProcessors(t)
	c, roots := stage7Heap(t, 4, 8448, 6, true)
	owner, child := roots[0], roots[1]
	source := &stage6OwnerEdges{edges: map[Ref][]Ref{owner: {child}, child: {owner}}}
	c.SetObjectEdgeSource(source)
	c.NewGlobalSlot(owner)
	before := *c.entry(owner)
	seen := map[Ref]bool{}
	remove := c.AddReclaimObserver(func(r Ref) {
		if seen[r] {
			t.Error("reclaimed twice")
		}
		seen[r] = true
		if p := c.parallel; p != nil && p.current != nil {
			for i := 0; i < p.participants; i++ {
				if !p.current.done[i] {
					t.Error("reclaimed before join")
				}
			}
		}
	})
	defer remove()
	c.throughput.mature.pressure = true
	if err := c.CollectFull(RefSliceRoots{owner}); err != nil {
		t.Fatal(err)
	}
	if c.entry(owner).off == before.off || !c.validObjectRef(child) || source.calls == 0 || len(seen) != 8446 {
		t.Fatal("sidecar edge/movement/callback failure", c.CollectionDiagnostics())
	}
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if !seen[owner] || !seen[child] || c.Stats().LiveObjects != 0 {
		t.Fatal("conditional cycle retained")
	}
}
