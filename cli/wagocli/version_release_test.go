//go:build !wago_lean

package wagocli

import "testing"

func TestReleaseMetadataFormatting(t *testing.T) {
	if got := releaseDate("2026-07-11T18:32:05Z"); got != "2026-07-11" {
		t.Fatalf("releaseDate = %q", got)
	}
	if got := shortHash("1234567890abcdef"); got != "1234567" {
		t.Fatalf("shortHash = %q", got)
	}
}
