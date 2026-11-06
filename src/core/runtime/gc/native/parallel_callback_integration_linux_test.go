//go:build linux && !tinygo && !wago_target_tinygo

package gc

import "testing"

type stage6OwnerEdges struct {
	edges map[Ref][]Ref
	calls int
}

func (s *stage6OwnerEdges) ObjectEdgeCount(r Ref) uint32               { s.calls++; return uint32(len(s.edges[r])) }
func (s *stage6OwnerEdges) ObjectEdgeAt(r Ref, i uint32) Ref           { s.calls++; return s.edges[r][i] }
func (*stage6OwnerEdges) PersistentRootIsStrong(SlotKind, uint32) bool { return false }

func TestParallelStage6PortableOwnerCallbacks(t *testing.T) {
	parallelTestProcessors(t)
	for _, shift := range []uint8{0, 6, 12} {
		c, parent, _ := newParallelTestGraph(t, 4, false, shift)
		scalar, err := NewStructDesc(2, []StorageKind{StorageI64})
		if err != nil {
			t.Fatal(err)
		}
		if err = c.AddTypes([]TypeDesc{scalar}); err != nil {
			t.Fatal(err)
		}
		owner, err := c.NewStructDefault(2)
		if err != nil {
			t.Fatal(err)
		}
		child, err := c.NewStructDefault(2)
		if err != nil {
			t.Fatal(err)
		}
		source := &stage6OwnerEdges{edges: map[Ref][]Ref{parent: {owner}, owner: {child}, child: {owner}}}
		c.SetObjectEdgeSource(source)
		c.NewGlobalSlot(owner) // conditional owner slot must not root the cycle alone
		p := c.parallelPool()
		traceDispatch, sweepDispatch := 0, 0
		p.hook = func(phase uint8, w int) {
			if w == 0 {
				if phase == parallelTrace {
					traceDispatch++
				}
				if phase == parallelSweep {
					sweepDispatch++
				}
			}
		}
		wantReclaims := int(c.Stats().LiveObjects)
		seen := map[Ref]bool{}
		remove := c.AddReclaimObserver(func(r Ref) {
			if r == Null() {
				return
			}
			if seen[r] {
				t.Errorf("duplicate reclaim %v", r)
			}
			seen[r] = true
			if p.current != nil {
				for i := 0; i < p.participants; i++ {
					if !p.current.done[i] {
						t.Errorf("callback before join worker %d", i)
					}
				}
			}
		})
		roots := RefSliceRoots{parent}
		if err = c.CollectFull(roots); err != nil {
			t.Fatal(err)
		}
		if traceDispatch != 0 || sweepDispatch == 0 || source.calls == 0 {
			t.Fatal("callback serialization/sweep test vacuous")
		}
		for range 3 {
			if err = c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
		}
		if seen[owner] || seen[child] {
			t.Fatal("external scalar edges lost")
		}
		// Drop strong owner; the conditional persistent slot and cycle alone cannot retain it.
		if err = c.CollectFull(nil); err != nil {
			t.Fatal(err)
		}
		if !seen[owner] || !seen[child] || c.Stats().LiveObjects != 0 || len(seen) != wantReclaims {
			t.Fatal("conditional owner cycle retained")
		}
		remove()
		c.Close()
	}
}
