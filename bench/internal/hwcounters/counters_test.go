package hwcounters

import "testing"

func TestCountScaledForMultiplexing(t *testing.T) {
	if got := (Count{Value: 75, TimeEnabled: 200, TimeRunning: 100}).Scaled(); got != 150 {
		t.Fatalf("scaled count = %v, want 150", got)
	}
	if got := (Count{Value: 75, TimeEnabled: 200}).Scaled(); got != 0 {
		t.Fatalf("zero-running scaled count = %v, want 0", got)
	}
}
