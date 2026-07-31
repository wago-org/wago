package plugin

import (
	"bytes"
	"testing"
	"time"
)

func TestSummary(t *testing.T) {
	var output bytes.Buffer
	PrintSummary(&output, []SummaryPackage{
		{Module: "github.com/wago-org/wasi", Version: "0.0.0"},
		{Module: "example.com/acme/log", Version: "1.2.3"},
	}, 2400*time.Microsecond)
	want := "\n+ wago-org/wasi@0.0.0\n+ example.com/acme/log@1.2.3\n\n2 packages installed [2.4ms]\n"
	if got := output.String(); got != want {
		t.Fatalf("package install summary = %q, want %q", got, want)
	}
}

func TestDisplayVersion(t *testing.T) {
	tests := map[string]string{
		"":                                   "0.0.0",
		"v1.2.3":                             "1.2.3",
		"v0.0.0-20260730120000-0123456789ab": "0.0.0",
		" v2.0.0-beta.1 ":                    "2.0.0-beta.1",
	}
	for input, want := range tests {
		if got := DisplayVersion(input); got != want {
			t.Errorf("DisplayVersion(%q) = %q, want %q", input, got, want)
		}
	}
}
