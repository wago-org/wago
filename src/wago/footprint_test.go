package wago

import "testing"

func requireBoundedInstanceFootprint(t *testing.T, got uintptr) {
	t.Helper()
	// Go 1.22 and Go 1.26 lay out synchronization primitives differently.
	// Packing lifecycle booleans around the arena-backed native-context pointer
	// keeps both supported layouts below the prior 864-byte ceiling.
	if got != 776 && got != 856 {
		t.Fatalf("Instance size = %d, want supported 776- or 856-byte layout", got)
	}
}
