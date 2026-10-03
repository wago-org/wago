//go:build linux && tinygo && wago_lean && wago_minimal

package atomicfile

import "testing"

func TestMinimalTinyBuildOnlyOptionsFailClosed(t *testing.T) {
	file, reservation, err := createRetainedReplacementTemp("ignored", true)
	if err == nil || file != nil || reservation.valid() {
		t.Fatalf("retained staging = (%v, valid %t, %v), want unavailable", file, reservation.valid(), err)
	}
	if _, err := probeUmaskMode("ignored", 0o644, true); err == nil {
		t.Fatal("umask probing unexpectedly succeeded")
	}
}
