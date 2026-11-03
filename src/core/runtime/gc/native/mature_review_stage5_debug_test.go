//go:build wagodebug

package gc

import (
	"fmt"
	"reflect"
	"testing"
)

func TestMatureReviewStage5MixedDestinationFailure(t *testing.T) {
	for _, point := range []failurePoint{failPromotionDestination, failPromotionCommit} {
		for _, after := range []int{0, 1, 2, 4, 5} {
			t.Run(fmt.Sprintf("%d/%d", point, after), func(t *testing.T) {
				c := stage4Collector(t, 6)
				var seed [3]Ref
				roots := Slots{}
				for i := range seed {
					roots = append(roots, (*Root)(&seed[i]))
					seed[i] = regionStruct(t, c, 0, roots)
				}
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				anchor := seed[2]
				roots = Slots{(*Root)(&anchor)}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				var refs [6]Ref
				for i := range refs {
					roots = append(roots, (*Root)(&refs[i]))
					refs[i] = regionStruct(t, c, 0, roots)
					if err := c.StructSet(refs[i], 1, I64Value(int64(100+i))); err != nil {
						t.Fatal(err)
					}
				}
				entries := append([]handleEntry(nil), c.handles...)
				nursery := append([]byte(nil), c.nursery...)
				bump := c.nurseryBump
				cleanup := armFailure(c, point, after)
				err := c.CollectMinor(roots)
				cleanup()
				if !isInjectedFailure(err) {
					t.Fatal("missing failure", err)
				}
				if !reflect.DeepEqual(entries, c.handles) || !reflect.DeepEqual(nursery, c.nursery) || bump != c.nurseryBump {
					t.Fatal("premature source publication")
				}
				matureReviewOracle(t, &c.throughput, []matureReviewExtent{{*c.entry(anchor), 32}})
				if !c.throughput.mature.refused || c.throughput.mature.cause == nil {
					t.Fatal("lost refusal")
				}
				if err = c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				live := []matureReviewExtent{{*c.entry(anchor), 32}}
				for i, r := range refs {
					live = append(live, matureReviewExtent{*c.entry(r), 32})
					v, err := c.StructGet(r, 1)
					if err != nil || v.I64() != int64(100+i) {
						t.Fatal("payload", err)
					}
				}
				matureReviewOracle(t, &c.throughput, live)
				if c.throughput.mature.refused || c.throughput.mature.cause != nil {
					t.Fatal("stale retry refusal")
				}
			})
		}
	}
}

func TestMatureReviewStage5PartialRetirementRetry(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 4096, NurseryRegionShift: 6, DisableMovingNursery: true, ThroughputHeapBytes: 65536, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	var refs [5]Ref
	roots := Slots{}
	for i := range refs {
		roots = append(roots, (*Root)(&refs[i]))
		var err error
		refs[i], err = c.NewArrayDefaultWithRoots(2, 2048, roots)
		if err != nil {
			t.Fatal(err)
		}
	}
	roots = Slots{(*Root)(&refs[1]), (*Root)(&refs[3])}
	cleanup := armFailure(&c.throughput, failThroughputReconciliation, 1)
	err := c.CollectFull(roots)
	cleanup()
	if !isInjectedFailure(err) {
		t.Fatal("missing partial reconciliation failure", err)
	}
	if c.throughput.mature.retired != 0 || len(c.throughput.pendingFree) != 2 {
		t.Fatalf("premature epochs/lost debt: retired %d pending %d", c.throughput.mature.retired, len(c.throughput.pendingFree))
	}
	live := []matureReviewExtent{{*c.entry(refs[1]), 8208}, {*c.entry(refs[3]), 8208}}
	matureReviewOracle(t, &c.throughput, live)
	// Retry through allocation first: reconciliation can finish before the next
	// major commits candidate epochs. Reuse the hole without losing live neighbors.
	r, err := c.NewArrayDefaultWithRoots(2, 2048, roots)
	if err != nil {
		t.Fatal(err)
	}
	roots = append(roots, (*Root)(&r))
	live = append(live, matureReviewExtent{*c.entry(r), 8208})
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	matureReviewOracle(t, &c.throughput, live)
	if err = c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	matureReviewOracle(t, &c.throughput, nil)
	if c.throughput.bump != 0 {
		t.Fatal("unreclaimed retry")
	}
}
