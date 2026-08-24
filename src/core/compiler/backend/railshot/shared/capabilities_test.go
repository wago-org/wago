package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/optimization"
)

func TestCompiledCapabilitiesMatchPolicy(t *testing.T) {
	for _, objective := range []OptimizationObjective{OptimizeSpeed, OptimizeBalanced, OptimizeSize, OptimizeEmbedded} {
		policy := CodegenPolicyForObjective(optimization.Selection{}, objective)
		if policy.Capabilities != CompiledCapabilities {
			t.Fatalf("%v policy capabilities = %#x, want %#x", objective, policy.Capabilities, CompiledCapabilities)
		}
		wantCompact := CompiledCapabilities.Has(CapabilityNativeCompaction) &&
			(objective == OptimizeSize || objective == OptimizeEmbedded)
		if policy.CompactNative != wantCompact {
			t.Fatalf("%v compact = %t, want %t", objective, policy.CompactNative, wantCompact)
		}
	}
}
