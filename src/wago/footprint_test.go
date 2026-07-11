package wago

import "testing"

func requireBoundedInstanceFootprint(t *testing.T, got uintptr) {
	t.Helper()
	// Go 1.22 and Go 1.26 lay out synchronization primitives differently.
	// Both supported layouts remain below the fixed 864-byte ceiling.
	if got != 784 && got != 864 {
		t.Fatalf("Instance size = %d, want supported 784- or 864-byte layout", got)
	}
}
