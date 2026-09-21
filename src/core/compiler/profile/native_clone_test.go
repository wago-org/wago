package profile

import (
	"crypto/sha256"
	"testing"
)

func TestPlanNativeClonesRequiresOpportunityAndClosesDirectCalls(t *testing.T) {
	p := Module{Version: Version, ModuleHash: sha256.Sum256([]byte("native-clone")), Source: SourceStatic, Phase: PhaseSteady, FunctionCounts: []uint64{0, 1000, 900, 800, 700}}
	calls := [][]uint32{nil, {2}, {3}, nil, nil}
	opportunities := []FunctionOpportunity{
		{},
		{BodyBytes: 10, GainPermille: 100, NativeOpportunity: true},
		{BodyBytes: 20},
		{BodyBytes: 30},
		{BodyBytes: 5, GainPermille: 20, NativeOpportunity: true},
	}
	plan, err := PlanNativeClones(p, 1, calls, opportunities, SelectionPolicy{MaxFunctions: 3, MaxBodyBytes: 60, MinimumCount: 100, MinimumGainPermille: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Roots) != 1 || plan.Roots[0] != 1 || len(plan.Functions) != 3 || plan.Functions[0] != 1 || plan.Functions[1] != 2 || plan.Functions[2] != 3 {
		t.Fatalf("native clone plan = %#v", plan)
	}
}

func TestPlanNativeClonesSkipsWholeClosureWhenBudgetDoesNotFit(t *testing.T) {
	p := Module{Version: Version, ModuleHash: sha256.Sum256([]byte("native-clone-budget")), Source: SourceStatic, Phase: PhaseSteady, FunctionCounts: []uint64{1000, 900, 800}}
	calls := [][]uint32{{1}, nil, nil}
	opportunities := []FunctionOpportunity{
		{BodyBytes: 20, GainPermille: 100, NativeOpportunity: true},
		{BodyBytes: 20},
		{BodyBytes: 10, GainPermille: 80, NativeOpportunity: true},
	}
	plan, err := PlanNativeClones(p, 0, calls, opportunities, SelectionPolicy{MaxFunctions: 1, MaxBodyBytes: 20, MinimumCount: 1, MinimumGainPermille: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Truncated || len(plan.Roots) != 1 || plan.Roots[0] != 2 || len(plan.Functions) != 1 || plan.Functions[0] != 2 {
		t.Fatalf("budgeted native clone plan = %#v", plan)
	}
}

func TestPlanNativeClonesRejectsUnboundedOrMalformedInputs(t *testing.T) {
	p := Module{Version: Version, ModuleHash: sha256.Sum256([]byte("native-clone-invalid")), Source: SourceStatic, Phase: PhaseSteady, FunctionCounts: []uint64{1}}
	opportunities := []FunctionOpportunity{{BodyBytes: 1, GainPermille: 1, NativeOpportunity: true}}
	for _, policy := range []SelectionPolicy{
		{},
		{MaxFunctions: 1, MaxBodyBytes: 1, MinimumCount: 1},
		{MaxFunctions: MaxRailshotFunctionCounters + 1, MaxBodyBytes: 1, MinimumCount: 1, MinimumGainPermille: 1},
	} {
		if _, err := PlanNativeClones(p, 0, [][]uint32{nil}, opportunities, policy); err == nil {
			t.Fatalf("unbounded policy %#v unexpectedly accepted", policy)
		}
	}
	if _, err := PlanNativeClones(p, 0, [][]uint32{{1}}, opportunities, SelectionPolicy{MaxFunctions: 1, MaxBodyBytes: 1, MinimumCount: 1, MinimumGainPermille: 1}); err == nil {
		t.Fatal("out-of-range call unexpectedly accepted")
	}
}
