//go:build wago_lean && !wago_railshot_compact && !wago_railshot_needles && !wago_railshot_gcopt && !wago_railshot_full

package wago

import (
	"strings"
	"testing"
)

func TestLeanOptimizationCapabilities(t *testing.T) {
	cfg := NewRuntimeConfig()
	for _, name := range []string{"shared-trap-body", "shared-adapters", "simd-superopt", "swar-idioms"} {
		for _, info := range cfg.OptimizationInfos() {
			if info.Name == name {
				if info.On {
					t.Errorf("lean default %q is enabled", name)
				}
				if info.Available {
					t.Errorf("lean optimization %q reports available", name)
				}
			}
		}
	}

	if err := cfg.WithOptimization("swar-idioms", true).Validate(); err == nil || !strings.Contains(err.Error(), "wago_railshot_needles") {
		t.Fatalf("unavailable needle validation = %v", err)
	}
	if err := cfg.WithOptimization("swar-idioms", false).Validate(); err != nil {
		t.Fatalf("disabling unavailable needle: %v", err)
	}
	if err := cfg.WithOptimizationObjective(OptimizeSize).Validate(); err != nil {
		t.Fatalf("capability-aware Size profile: %v", err)
	}
}
