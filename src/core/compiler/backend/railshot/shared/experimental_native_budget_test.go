package shared

import "testing"

func TestExperimentalNativeBudget(t *testing.T) {
	for _, n := range []int{-1, 0, 8191, 8192, 8193, 262143, 262144, 262145} {
		if got, want := ExperimentalNativeFunctionBudget(n), n >= 0 && n <= 8192; got != want {
			t.Fatal("function", n, got, want)
		}
		if got, want := ExperimentalNativeModuleBudget(n), n >= 0 && n <= 262144; got != want {
			t.Fatal("module", n, got, want)
		}
	}
}
