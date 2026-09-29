//go:build arm64 && !wago_profile && !wago_gcstats && !wago_codegenstats

package arm64

import (
	"strings"
	"testing"
)

func TestCompilerDiagnosticsRequireBuildTag(t *testing.T) {
	for _, opts := range []CompileOptions{{Stats: &ModuleStats{}}, {CollectInlineReport: true}} {
		_, err := CompileModuleWith(nil, opts)
		if err == nil || !strings.Contains(err.Error(), "wago_codegenstats") {
			t.Fatalf("expected explicit unavailable-diagnostics error, got %v", err)
		}
	}
	t.Setenv("WAGO_EXPLAIN", "1")
	if got := diagnosticEnv("WAGO_EXPLAIN"); got != "" {
		t.Fatalf("ordinary build read diagnostic environment: %q", got)
	}
}
