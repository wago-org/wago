//go:build tinygo || wago_target_tinygo

package gc

import "testing"

func TestEvacuationTinyGoPolicy(t *testing.T) {
	for _, workers := range []int{0, 1, 2, 4, 8} {
		err := ValidateConfig(Config{Workers: workers, NurseryBytes: 4096, NurseryRegionShift: 6, MatureEvacuation: true})
		if (err != nil) != (workers > 1) {
			t.Fatalf("workers %d: %v", workers, err)
		}
	}
}
