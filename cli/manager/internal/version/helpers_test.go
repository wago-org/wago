package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestVersionSelectionAndOrderingHelpers(t *testing.T) {
	if !isRollingChannel("canary") || isRollingChannel("1.2.3") || channelRelease("nightly-20260101") != "nightly" || channelRelease("v1.2.3") != "" {
		t.Fatal("release channel detection mismatch")
	}
	if got := strings.Join(stableReleaseNames([]string{"v1.2.3", "canary-abcd", "", "nightly-2026"}), ","); got != "v1.2.3" {
		t.Fatalf("stable releases = %q", got)
	}
	for _, tc := range []struct {
		active          string
		args            []string
		nightly, canary bool
		want            string
		err             bool
	}{
		{"canary", nil, false, false, "canary", false},
		{"", nil, true, false, "nightly", false},
		{"", []string{"nightly"}, false, false, "nightly", false},
		{"1.2.3", nil, false, false, "", true},
		{"", []string{"1", "2"}, false, false, "", true},
		{"", nil, true, true, "", true},
	} {
		got, err := updateVersionTarget(tc.active, tc.args, tc.nightly, tc.canary)
		if (err != nil) != tc.err || got != tc.want {
			t.Fatalf("updateVersionTarget(%q, %v) = %q, %v", tc.active, tc.args, got, err)
		}
	}
}

func TestVersionStateAndBuildFileHelpers(t *testing.T) {
	root := t.TempDir()
	d := wago.Dirs{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache")}
	for _, v := range []string{"v1.9.0", "v1.10.0"} {
		if err := os.MkdirAll(filepath.Dir(d.VersionBinary(v)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d.VersionBinary(v), []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(installedVersions(d), ","); got != "v1.9.0,v1.10.0" {
		t.Fatalf("installed versions = %q", got)
	}
	if err := setActiveVersion(d, "v1.10.0"); err != nil || activeVersion(d) != "v1.10.0" {
		t.Fatalf("active version = %q, %v", activeVersion(d), err)
	}

}
