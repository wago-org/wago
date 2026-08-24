//go:build wago_lean && wago_railshot_gcopt && !wago_railshot_compact && !wago_railshot_needles && !wago_railshot_full

package shared

import "testing"

func TestGCOptBuildCapabilities(t *testing.T) {
	if CompiledCapabilities != CapabilityNativeGCOptimizations {
		t.Fatalf("GC optimizer capabilities = %#x, want %#x", CompiledCapabilities, CapabilityNativeGCOptimizations)
	}
}
