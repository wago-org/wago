//go:build linux && !tinygo && !wago_target_tinygo

package gc

import (
	"fmt"
	"slices"
	"testing"
)

func TestMinorReclaimObserverPrecedesIdentityReuse(t *testing.T) {
	if !nativeCollectorFastPaths {
		t.Skip("minor recycling shortcuts unavailable")
	}
	for _, tc := range []struct {
		name     string
		count    int
		live     int
		shortcut string
	}{
		{name: "ordinary mixed", count: 16, live: 4, shortcut: "ordinary"},
		{name: "sparse mixed", count: 16, live: 1, shortcut: "sparse"},
		{name: "entirely dead", count: 16, shortcut: "all-dead"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCollector(t, Config{
				NurseryBytes:         4096,
				SurvivorBytes:        2048,
				ThroughputHeapBytes:  4096,
				ThroughputPageBytes:  4096,
				ThroughputClassLimit: 4096,
				PoisonFreed:          false,
				VerifyAfterCollect:   true,
			})
			if err := c.EnableCheckedHandles(); err != nil {
				t.Fatal(err)
			}
			refs := make([]Ref, tc.count)
			generations := make(map[uint32]uint64, tc.count)
			shadowOwner := make(map[Ref]string, tc.count)
			for i := range refs {
				var err error
				refs[i], err = c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				generations[handleOf(refs[i])], err = c.CheckedIdentity(refs[i])
				if err != nil {
					t.Fatal(err)
				}
				shadowOwner[refs[i]] = fmt.Sprintf("old-%d", i)
			}
			if c.cfg.PoisonFreed || len(c.nurseryHandles) != tc.count {
				t.Fatalf("shortcut preconditions: poison=%v nursery=%d, want false/%d", c.cfg.PoisonFreed, len(c.nurseryHandles), tc.count)
			}
			switch tc.shortcut {
			case "ordinary":
				if tc.live <= tc.count/8 || tc.live == tc.count {
					t.Fatal("ordinary shortcut fixture does not exceed sparse threshold")
				}
			case "sparse":
				if tc.live == 0 || tc.live > tc.count/8 {
					t.Fatal("sparse shortcut fixture is outside sparse threshold")
				}
			case "all-dead":
				if tc.live != 0 {
					t.Fatal("all-dead shortcut fixture has roots")
				}
			}

			calls := make(map[Ref]int, tc.count-tc.live)
			remove := c.AddReclaimObserver(func(ref Ref) {
				if _, ok := shadowOwner[ref]; !ok {
					t.Errorf("reclaim %v observed after old ownership was absent", ref)
				}
				calls[ref]++
				delete(shadowOwner, ref)
			})
			defer remove()
			roots := RefSliceRoots(refs[:tc.live])
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			dead := make(map[uint32]Ref, tc.count-tc.live)
			for i, ref := range refs {
				if i < tc.live {
					if calls[ref] != 0 || shadowOwner[ref] == "" {
						t.Fatalf("live identity %v was reclaimed or lost ownership", ref)
					}
					continue
				}
				if calls[ref] != 1 {
					t.Fatalf("old identity %v notifications = %d, want 1", ref, calls[ref])
				}
				if _, ok := shadowOwner[ref]; ok {
					t.Fatalf("old identity %v retained shadow ownership", ref)
				}
				dead[handleOf(ref)] = ref
			}
			for range len(dead) {
				fresh, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				h := handleOf(fresh)
				old, reused := dead[h]
				if !reused {
					t.Fatalf("new identity %v did not reuse a dead handle", fresh)
				}
				if _, inherited := shadowOwner[fresh]; inherited {
					t.Fatalf("new identity %v inherited old shadow ownership", fresh)
				}
				generation, err := c.CheckedIdentity(fresh)
				if err != nil {
					t.Fatal(err)
				}
				if generation != generations[h]+1 {
					t.Fatalf("reused handle %d generation = %d, want %d", h, generation, generations[h]+1)
				}
				if calls[old] != 1 {
					t.Fatalf("identity %v notification changed during reuse: %d", old, calls[old])
				}
				delete(dead, h)
			}
		})
	}
}

func TestDeadNurseryReclaimObserverPanicPrecedesIdentityMutation(t *testing.T) {
	if !nativeCollectorFastPaths {
		t.Skip("dead-nursery recycling shortcut unavailable")
	}
	c := newTestCollector(t, Config{NurseryBytes: 4096, SurvivorBytes: 2048})
	for range 8 {
		if _, err := c.NewStructDefault(0); err != nil {
			t.Fatal(err)
		}
	}
	beforeHandles := slices.Clone(c.handles)
	beforeFree := slices.Clone(c.freeHandles)
	beforeNursery := slices.Clone(c.nurseryHandles)
	remove := c.AddReclaimObserver(func(Ref) { panic("minor reclaim sentinel") })
	defer remove()
	func() {
		defer func() {
			if got := recover(); got != "minor reclaim sentinel" {
				t.Fatalf("panic = %v", got)
			}
		}()
		_ = c.CollectMinor(nil)
	}()
	if !slices.Equal(c.handles, beforeHandles) || !slices.Equal(c.freeHandles, beforeFree) || !slices.Equal(c.nurseryHandles, beforeNursery) {
		t.Fatal("observer panic mutated handle identity state")
	}
}
