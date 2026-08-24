//go:build !wago_lean

package shared

import "testing"

func TestStandardBuildCapabilities(t *testing.T) {
	want := ProductionCapabilities
	if CompiledCapabilities != want {
		t.Fatalf("standard capabilities = %#x, want %#x", CompiledCapabilities, want)
	}
}
