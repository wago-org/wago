//go:build wago_lean && wago_railshot_needles && !wago_railshot_compact && !wago_railshot_gcopt && !wago_railshot_full

package shared

import "testing"

func TestNeedlesBuildCapabilities(t *testing.T) {
	if CompiledCapabilities != CapabilityProducerNeedles {
		t.Fatalf("needle capabilities = %#x, want %#x", CompiledCapabilities, CapabilityProducerNeedles)
	}
}
