//go:build !wago_profile && !wago_gcstats && !wago_codegenstats

package wago

import (
	"strings"
	"testing"
)

func TestCompilerTelemetryRequiresBuildTag(t *testing.T) {
	err := NewRuntimeConfig().WithGCCodeTelemetry(true).Validate()
	if err == nil || !strings.Contains(err.Error(), "wago_codegenstats") {
		t.Fatalf("expected explicit unavailable-telemetry error, got %v", err)
	}
}
