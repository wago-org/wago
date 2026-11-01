//go:build tinygo || wago_target_tinygo

package gc

import "testing"

func TestEvacuationStage7DisabledTarget(t *testing.T) {
	if err := ValidateConfig(Config{NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true}); err == nil {
		t.Fatal("unqualified evacuation target accepted")
	}
}
