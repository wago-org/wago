//go:build wagodebug

package gc

import (
	"fmt"
	"slices"
	"testing"
)

func TestRFWGCMatureStage4LateRollback(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		for _, point := range []failurePoint{failPromotionPlan, failPromotionDestination, failPromotionCommit} {
			t.Run(fmt.Sprintf("%d/%d", shift, point), func(t *testing.T) {
				c := stage4Collector(t, shift)
				// Three dead slots below a live anchor force reuse of an
				// interior span; full reconciliation cannot trim the bump.
				var seed [4]Ref
				seedRoots := Slots{}
				for i := range seed {
					seedRoots = append(seedRoots, (*Root)(&seed[i]))
					seed[i] = regionStruct(t, c, 0, seedRoots)
				}
				if err := c.CollectMinor(seedRoots); err != nil {
					t.Fatal(err)
				}
				anchor := seed[3]
				roots := Slots{(*Root)(&anchor)}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				var refs [3]Ref
				for i := range refs {
					roots = append(roots, (*Root)(&refs[i]))
					refs[i] = regionStruct(t, c, 0, roots)
					if err := c.StructSet(refs[i], 1, I64Value(int64(100+i))); err != nil {
						t.Fatal(err)
					}
				}
				entries := slices.Clone(c.handles)
				nurseryBump := c.nurseryBump
				bump := c.throughput.bump
				cleanup := armFailure(c, point, 1)
				err := c.CollectMinor(roots)
				cleanup()
				if !isInjectedFailure(err) {
					t.Fatalf("missing injected refusal: %v", err)
				}
				if !slices.Equal(entries, c.handles) || c.nurseryBump != nurseryBump || c.throughput.bump != bump {
					t.Fatalf("partial transaction publication: entries %v -> %v; nursery bump %d -> %d; mature bump %d -> %d", entries, c.handles, nurseryBump, c.nurseryBump, bump, c.throughput.bump)
				}
				if err := c.Verify(roots); err != nil {
					t.Fatal(err)
				}
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal("retry", err)
				}
				live := map[Ref]uint32{anchor: 32}
				for i, r := range refs {
					live[r] = 32
					v, err := c.StructGet(r, 1)
					if err != nil || v.I64() != int64(100+i) {
						t.Fatal("payload", v, err)
					}
				}
				stage4Oracle(t, c, live)
			})
		}
	}
}
