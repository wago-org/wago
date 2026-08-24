//go:build wago_lean && wago_railshot_compact && !wago_railshot_needles && !wago_railshot_gcopt && !wago_railshot_full

package shared

import "testing"

func TestCompactBuildCapability(t *testing.T) {
	if CompiledCapabilities != CapabilityNativeCompaction {
		t.Fatalf("compact capabilities = %#x, want %#x", CompiledCapabilities, CapabilityNativeCompaction)
	}
}
