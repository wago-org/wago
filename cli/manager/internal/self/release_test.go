package self

import "testing"

func TestChannelPreservesReleaseTrack(t *testing.T) {
	tests := map[string]string{
		"canary": "canary",
		"v0.1.0-canary.g7d8c58a000000000000000000000000000000000": "canary",
		"canary@7d8c58a000000000000000000000000000000000":         "canary",
		"beta":          "beta",
		"v0.1.0-beta.2": "beta",
		"beta@7d8c58a000000000000000000000000000000000": "beta",
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
