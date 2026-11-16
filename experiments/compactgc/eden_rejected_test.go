//go:build !wago_gc_compact_eden || wago_gc_compact_cohort || wago_gc_compact_shadow || wago_gc_compact_age2_metrics || wago_gc_compact_age2_queue_reuse || wago_gc_compact8

package compactgc

import "testing"

func TestEdenUnsupportedBuildRejected(t *testing.T) {
	if _, err := NewEdenWithTypes(128, 32, 128, nil); err != ErrConfig {
		t.Fatal(err)
	}
}
