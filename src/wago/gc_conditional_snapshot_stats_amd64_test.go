//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_gcstats

package wago

import (
	"fmt"
	"testing"
)

func TestGCConditionalSnapshotExecutesHelperFallback(t *testing.T) {
	for _, array := range []bool{false, true} {
		for _, repeats := range []int{1, 64} {
			t.Run(fmt.Sprintf("array=%t/stores=%d", array, repeats), func(t *testing.T) {
				runConditionalSnapshotStore(t, array, 19, repeats, func(t *testing.T, in *Instance) {
					t.Helper()
					stats := in.GCHelperStats()
					if stats.Calls != 1 || stats.MutationCalls != 1 || stats.OldYoungUnrememberedCalls != 1 {
						t.Fatalf("executed helpers = %+v; want exactly one unremembered old-to-young mutation", stats)
					}
					if array && (stats.ArrayMutationCalls != 1 || stats.ArrayCardPresentCalls != 0) {
						t.Fatalf("array fallback = %+v; want one mutation without a prior card", stats)
					}
					if !array && stats.StructMutationCalls != 1 {
						t.Fatalf("struct fallback = %+v", stats)
					}
				})
			})
		}
	}
}
