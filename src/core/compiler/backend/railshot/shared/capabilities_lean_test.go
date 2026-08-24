//go:build wago_lean && !wago_railshot_compact && !wago_railshot_needles && !wago_railshot_gcopt && !wago_railshot_full

package shared

import "testing"

func TestLeanBuildCapabilities(t *testing.T) {
	if CompiledCapabilities != 0 {
		t.Fatalf("lean capabilities = %#x, want none", CompiledCapabilities)
	}
}
