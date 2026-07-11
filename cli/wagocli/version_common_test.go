package wagocli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

// installFake writes a fake installed binary for ver under d.
func installFake(t *testing.T, d wago.Dirs, ver string) {
	t.Helper()
	dir := filepath.Join(d.Versions, ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.VersionBinary(ver), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestVersionManagerState(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	d := wago.DirsFor("test")

	if got := installedVersions(d); len(got) != 0 {
		t.Fatalf("expected no versions, got %v", got)
	}
	installFake(t, d, "0.3.0")
	installFake(t, d, "0.5.0")
	installFake(t, d, "0.10.0")

	got := installedVersions(d)
	want := []string{"0.3.0", "0.5.0", "0.10.0"} // numeric semver order, not lexical
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("installedVersions = %v, want %v", got, want)
	}

	if activeVersion(d) != "" {
		t.Fatal("expected no active version")
	}
	if err := setActiveVersion(d, "0.5.0"); err != nil {
		t.Fatalf("setActiveVersion: %v", err)
	}
	if activeVersion(d) != "0.5.0" {
		t.Fatalf("activeVersion = %q, want 0.5.0", activeVersion(d))
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.5.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.0", "1.2.1", -1},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Fatalf("compareSemver(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestUpdateVersionTarget(t *testing.T) {
	tests := []struct {
		name            string
		active          string
		args            []string
		nightly, canary bool
		want            string
		wantErr         string
	}{
		{name: "active", active: "0.5.0", want: "0.5.0"},
		{name: "named", active: "0.5.0", args: []string{"0.6.0"}, want: "0.6.0"},
		{name: "nightly", active: "0.5.0", nightly: true, want: "nightly"},
		{name: "canary", active: "0.5.0", canary: true, want: "canary"},
		{name: "missing active", wantErr: "no active version"},
		{name: "both channels", nightly: true, canary: true, wantErr: "cannot be used together"},
		{name: "channel plus version", args: []string{"0.6.0"}, nightly: true, wantErr: "cannot be used with [version]"},
		{name: "too many versions", args: []string{"0.5.0", "0.6.0"}, wantErr: "at most one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := updateVersionTarget(tt.active, tt.args, tt.nightly, tt.canary)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("updateVersionTarget() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("updateVersionTarget() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("updateVersionTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}
