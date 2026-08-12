package version

import "testing"

func TestDiagnosticChannel(t *testing.T) {
	for _, test := range []struct {
		active, release, want string
	}{
		{"canary", "deadbee", "canary"},
		{"", "canary-20260729-deadbee", "canary"},
		{"", "canary@deadbee123456789012345678901234567890123", "canary"},
		{"nightly", "deadbee", "nightly"},
		{"", "nightly-20260729-deadbee", "nightly"},
		{"", "nightly@deadbee123456789012345678901234567890123", "nightly"},
		{"latest", "v1.0.0", "latest"},
		{"", "v1.0.0", "stable"},
		{"local", "deadbee", "local"},
		{"", "deadbee", "development"},
	} {
		if got := DiagnosticChannel(test.active, test.release); got != test.want {
			t.Fatalf("DiagnosticChannel(%q, %q) = %q, want %q", test.active, test.release, got, test.want)
		}
	}
}
