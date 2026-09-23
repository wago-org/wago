//go:build amd64 && tinygo && wago_minimal

package plugins

import (
	"strings"
	"testing"

	amd64codegen "github.com/wago-org/wago/codegen/amd64"
)

func TestMinimalTinyGoRejectsAVXPluginLowerings(t *testing.T) {
	for _, feature := range []amd64codegen.Features{amd64codegen.FeatureAVX2, amd64codegen.FeatureAVX512} {
		spec := InstructionSpec{
			Module: "test",
			Name:   "avx",
			Codegen: &amd64codegen.Lowering{
				Compatibility: amd64codegen.CompatibilityManaged,
				Features:      feature,
				Managed:       func(amd64codegen.ManagedContext) error { return nil },
			},
		}
		if _, err := prepareMachineCode(spec); err == nil || !strings.Contains(err.Error(), "AVX features unavailable") {
			t.Fatalf("feature %#x: error = %v", feature, err)
		}
	}
}
