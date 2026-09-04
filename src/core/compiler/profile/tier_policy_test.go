package profile

import (
	"crypto/sha256"
	"reflect"
	"testing"
)

func tierProfile(counts ...uint64) Module {
	return Module{Version: Version, ModuleHash: sha256.Sum256([]byte("tier")), Generation: 1, Source: SourceRailshot, Phase: PhaseSteady, FunctionCounts: counts}
}

func TestPlanTierSelectsHotRootsAndDirectCallClusters(t *testing.T) {
	p := tierProfile(9000, 80, 500, 1000, 20, 10)
	calls := [][]uint32{nil, nil, {4, 3}, {5}, nil, nil}
	plan, err := PlanTier(p, 1, calls, TierPolicy{MinCount: 100, MaxFunctions: 4, ClusterDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Roots, []uint32{3, 2}) {
		t.Fatalf("roots = %v, want [3 2]", plan.Roots)
	}
	if !reflect.DeepEqual(plan.Functions, []uint32{2, 3, 4, 5}) || plan.Truncated {
		t.Fatalf("functions = %v truncated=%v, want [2 3 4 5] false", plan.Functions, plan.Truncated)
	}
}

func TestPlanTierKeepsRootWhenClusterExceedsBound(t *testing.T) {
	p := tierProfile(1000, 900, 800, 700)
	calls := [][]uint32{{1, 2, 3}, nil, nil, nil}
	plan, err := PlanTier(p, 0, calls, TierPolicy{MinCount: 500, MaxFunctions: 2, ClusterDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Functions, []uint32{0, 1}) || !plan.Truncated {
		t.Fatalf("functions = %v truncated=%v, want [0 1] true", plan.Functions, plan.Truncated)
	}
}

func TestPlanTierRejectsMalformedGraphAndUnboundedPolicy(t *testing.T) {
	p := tierProfile(1)
	for _, test := range []struct {
		calls  [][]uint32
		policy TierPolicy
	}{
		{calls: nil, policy: TierPolicy{MinCount: 1, MaxFunctions: 1}},
		{calls: [][]uint32{{1}}, policy: TierPolicy{MinCount: 1, MaxFunctions: 1}},
		{calls: [][]uint32{nil}, policy: TierPolicy{MinCount: 0, MaxFunctions: 1}},
		{calls: [][]uint32{nil}, policy: TierPolicy{MinCount: 1, MaxFunctions: 0}},
		{calls: [][]uint32{nil}, policy: TierPolicy{MinCount: 1, MaxFunctions: 1, ClusterDepth: 9}},
	} {
		if _, err := PlanTier(p, 0, test.calls, test.policy); err == nil {
			t.Fatalf("PlanTier(%v, %#v) unexpectedly succeeded", test.calls, test.policy)
		}
	}
}
