package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/optimization"
)

func TestCompiledCapabilitiesMatchPolicy(t *testing.T) {
	for _, compact := range []bool{false, true} {
		policy := DefaultCodegenPolicy(optimization.Selection{})
		if compact {
			policy = CompactCodegenPolicy(optimization.Selection{})
		}
		if policy.Capabilities != CompiledCapabilities {
			t.Fatalf("compact=%t policy capabilities = %#x, want %#x", compact, policy.Capabilities, CompiledCapabilities)
		}
		wantCompact := compact && CompiledCapabilities.Has(CapabilityNativeCompaction)
		if policy.CompactNative != wantCompact {
			t.Fatalf("compact=%t policy compact = %t, want %t", compact, policy.CompactNative, wantCompact)
		}
	}
}
