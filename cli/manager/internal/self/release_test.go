package self

import "testing"

func TestChannelPreservesReleaseTrack(t *testing.T) {
	tests := map[string]string{
		"canary":                   "canary",
		"canary-7d8c58a":           "canary",
		"nightly":                  "nightly",
		"nightly-20260728-7d8c58a": "nightly",
		"v0.2.0":                   "latest",
		"0.0.0":                    "canary",
		"7d8c58a":                  "canary",
	}
	for version, want := range tests {
		if got := Channel(version); got != want {
			t.Errorf("Channel(%q) = %q, want %q", version, got, want)
		}
	}
}
