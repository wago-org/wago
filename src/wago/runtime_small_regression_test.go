package wago

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestGCInvocationDomainSetSortMatchesOrderedIDs(t *testing.T) {
	rng := rand.New(rand.NewSource(715))
	for _, n := range []int{0, 1, 2, 3, 4, 5, 8, 9, 16, 32, 64, 129} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			for trial := 0; trial < 32; trial++ {
				var set gcInvocationDomainSet
				for _, id := range rng.Perm(n) {
					set.add(&gcStoreDomain{id: uint64(id + 1)})
				}
				set.sort()
				if set.len() != n {
					t.Fatalf("length %d, want %d", set.len(), n)
				}
				for i := 0; i < n; i++ {
					if got := set.at(i).id; got != uint64(i+1) {
						t.Fatalf("position %d: ID %d, want %d", i, got, i+1)
					}
					// Sorting must preserve membership, including the overflow index.
					set.add(set.at(i))
					if set.len() != n {
						t.Fatal("sorting invalidated duplicate detection")
					}
				}
			}
		})
	}
}

func TestGCInvocationDomainSetInlineSortDoesNotAllocate(t *testing.T) {
	domains := [4]gcStoreDomain{{id: 4}, {id: 1}, {id: 3}, {id: 2}}
	for n := 0; n <= len(domains); n++ {
		allocs := testing.AllocsPerRun(100, func() {
			var set gcInvocationDomainSet
			for i := 0; i < n; i++ {
				set.add(&domains[i])
			}
			set.sort()
			for i := 1; i < n; i++ {
				if set.at(i-1).id >= set.at(i).id {
					t.Fatal("unordered inline set")
				}
			}
		})
		if allocs != 0 {
			t.Fatalf("N=%d: %g allocations, want zero", n, allocs)
		}
	}
}
