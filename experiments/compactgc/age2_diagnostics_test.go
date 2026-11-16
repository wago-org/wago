//go:build wago_gc_compact_age2 && (wago_gc_compact_cohort || wago_gc_compact_shadow)

package compactgc

import "testing"

func TestAge2RejectsPromoteAllDiagnostics(t *testing.T) {
	if c, err := NewAge2WithTypes(128, 256, 32, nil); err != ErrConfig || c != nil {
		t.Fatalf("unsupported diagnostic policy admitted: %v %v", c, err)
	}
	if c, err := NewWithTypes(128, 256, nil); err != nil || c.Age2Enabled() {
		t.Fatalf("ordinary diagnostic collector changed: %v", err)
	}
}
