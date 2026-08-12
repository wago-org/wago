package self

import "testing"

func TestChannelPreservesReleaseTrack(t *testing.T) {
	tests := map[string]string{
		"canary":         "canary",
		"canary-7d8c58a": "canary",
		"canary@7d8c58a000000000000000000000000000000000": "canary",
		"nightly":                  "nightly",
		"nightly-20260728-7d8c58a": "nightly",
		"nightly@7d8c58a000000000000000000000000000000000": "nightly",
		"v0.2.0":  "latest",
		"0.0.0":   "canary",
		"7d8c58a": "canary",
	}
	for version, want := range tests {
		if got := Channel(version); got != want {
			t.Errorf("Channel(%q) = %q, want %q", version, got, want)
		}
	}
}
