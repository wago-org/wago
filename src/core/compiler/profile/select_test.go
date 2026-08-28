package profile

import (
	"math"
	"testing"
)

func TestSelectFunctionsRequiresHeatOpportunityGainAndBudget(t *testing.T) {
	p := Module{FunctionCounts: []uint64{100, 90, 200, 80, 70}}
	opportunities := []FunctionOpportunity{
		{BodyBytes: 20, GainPermille: 50, NativeOpportunity: true},
		{BodyBytes: 10, GainPermille: 100, NativeOpportunity: true},
		{BodyBytes: 1, GainPermille: 500},
		{BodyBytes: 8, GainPermille: 20, NativeOpportunity: true},
		{BodyBytes: 9, GainPermille: 100, NativeOpportunity: true},
	}
	selected, err := SelectFunctions(p, opportunities, SelectionPolicy{
		MaxFunctions: 2, MaxBodyBytes: 25, MinimumCount: 50, MinimumGainPermille: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].Function != 1 || selected[1].Function != 4 {
		t.Fatalf("selection = %#v, want functions 1 and 4", selected)
	}
}

func TestSelectFunctionsRankingSaturatesAndRemainsStable(t *testing.T) {
	p := Module{FunctionCounts: []uint64{math.MaxUint64, math.MaxUint64}}
	opportunities := []FunctionOpportunity{
		{BodyBytes: 9, GainPermille: math.MaxUint16, NativeOpportunity: true},
		{BodyBytes: 8, GainPermille: math.MaxUint16, NativeOpportunity: true},
	}
	selected, err := SelectFunctions(p, opportunities, SelectionPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].Function != 1 || selected[1].Function != 0 {
		t.Fatalf("saturated selection = %#v", selected)
	}
	if _, err := SelectFunctions(Module{FunctionCounts: []uint64{1, 2}}, opportunities[:1], SelectionPolicy{}); err == nil {
		t.Fatal("profile/opportunity mismatch accepted")
	}
}
