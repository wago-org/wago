//go:build arm64

package arm64

import "testing"

func TestCallSafePinPoolExcludesCallerSavedRegistersAfterReservations(t *testing.T) {
	pool := []Reg{X19, X20, X21, X23, X24, X9, X10, X11, X15, X27}
	got := callSafePinPool(pool)
	want := [...]Reg{X19, X20, X21, X23, X24}
	if len(got) != len(want) {
		t.Fatalf("safe pool = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("safe pool = %v, want %v", got, want)
		}
	}
}

func TestLocalLoopPinThresholdChargesCallBoundaries(t *testing.T) {
	for _, tc := range []struct {
		reg       Reg
		callSites uint16
		want      uint32
	}{
		{reg: X15, callSites: 0, want: 0},
		{reg: X15, callSites: 6, want: 84},
		{reg: X27, callSites: 2, want: 200},
		{reg: X27, callSites: 6, want: 240},
	} {
		if got := localLoopPinThreshold(tc.reg, tc.callSites); got != tc.want {
			t.Errorf("threshold(%v, %d) = %d, want %d", tc.reg, tc.callSites, got, tc.want)
		}
	}
	if localLoopPinProfitable(X15, 6, 84) {
		t.Error("break-even X15 pin reported profitable")
	}
	if !localLoopPinProfitable(X15, 6, 85) {
		t.Error("profitable X15 pin rejected")
	}
}
