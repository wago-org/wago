//go:build wago_guardpage && (linux || darwin || windows) && (amd64 || arm64)

package runtime

import "testing"

func TestClosedGuardedMemoryDropsReservation(t *testing.T) {
	j, err := NewJobMemoryGuarded(65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if j.mem != nil || j.reserveBase != 0 || j.reserveLen != 0 || j.linOff != 0 || j.linLen != 0 {
		t.Fatal("closed guarded memory retains a native reservation")
	}
	if err := j.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
