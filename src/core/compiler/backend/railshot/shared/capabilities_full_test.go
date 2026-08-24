//go:build wago_lean && wago_railshot_full

package shared

import "testing"

func TestFullBuildCapabilities(t *testing.T) {
	if CompiledCapabilities != ProductionCapabilities {
		t.Fatalf("full capabilities = %#x, want %#x", CompiledCapabilities, ProductionCapabilities)
	}
}
