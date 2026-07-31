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

func TestRuntimeTargetFollowsActiveRollingChannel(t *testing.T) {
	tests := []struct {
		active, channel, resolved, want string
	}{
		{"canary", "canary", "canary@880e153000000000000000000000000000000000", "canary-880e153"},
		{"canary-6342d5e", "canary", "canary@880e153000000000000000000000000000000000", "canary-880e153"},
		{"nightly-20260729-6342d5e", "nightly", "nightly-20260730-880e153", "nightly-20260730-880e153"},
		{"nightly", "canary", "canary@880e153000000000000000000000000000000000", ""},
		{"v0.2.0", "latest", "v0.2.1", ""},
	}
	for _, test := range tests {
		if got := RuntimeTarget(test.active, test.channel, test.resolved); got != test.want {
			t.Errorf("RuntimeTarget(%q, %q, %q) = %q, want %q", test.active, test.channel, test.resolved, got, test.want)
		}
	}
}
