package shared

import "testing"

func TestQualityDebtAggregationAndHelperClassification(t *testing.T) {
	report := QualityDebtReport{Functions: []FunctionQualityDebt{
		{FrameBytes: 16, Spills: 2, Reloads: 1, BoundsChecks: 3, NativeBytes: 20},
		{FrameBytes: 32, Spills: 4, Reloads: 5, BoundsChecks: 6, NativeBytes: 40},
	}}
	got := report.Total()
	if got.FrameBytes != 48 || got.Spills != 6 || got.Reloads != 6 || got.BoundsChecks != 9 || got.NativeBytes != 60 {
		t.Fatalf("quality debt total = %#v", got)
	}
	calls := map[string]int{CallInline: 100, CallRegisterABI: 80, CallHost: 2, CallHostSync: 3, CallCrossInstance: 5, CallImportDispatch: 7, CallWrapper: 11, CallIndirect: 13}
	if got := HelperTransitionCount(calls); got != 28 {
		t.Fatalf("helper transitions = %d, want 28", got)
	}
}
