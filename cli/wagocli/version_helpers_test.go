package wagocli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestVersionSelectionAndOrderingHelpers(t *testing.T) {
	if compareSemver("v1.10.0", "1.9.9") <= 0 || compareSemver("1.0", "1.0.0") >= 0 || compareSemver("1.x", "1.2") <= 0 {
		t.Fatal("semver ordering mismatch")
	}
	if n, ok := atoiOK("012"); !ok || n != 12 {
		t.Fatalf("atoiOK = %d, %v", n, ok)
	}
	if _, ok := atoiOK("1x"); ok || get([]string{"a"}, 2) != "" || sign(-3) != -1 || sign(0) != 0 || sign(3) != 1 {
		t.Fatal("numeric helper behavior mismatch")
	}
	if !isRollingChannel("canary") || isRollingChannel("1.2.3") || channelRelease("nightly-20260101") != "nightly" || channelRelease("v1.2.3") != "" {
		t.Fatal("release channel detection mismatch")
	}
	if got := strings.Join(stableReleaseNames([]string{"v1.2.3", "canary-abcd", "", "nightly-2026"}), ","); got != "1.2.3" {
		t.Fatalf("stable releases = %q", got)
	}
	if got := strings.Join(remoteVersionNames([]string{"v1.2.3", "canary-a", "canary-b", "nightly-x"}), ","); got != "1.2.3,canary,nightly" {
		t.Fatalf("remote releases = %q", got)
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

	dir := t.TempDir()
	if err := writeBuildMain(dir, []string{"example.com/z", "example.com/a"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil || !strings.Contains(string(b), "example.com/a/register") || strings.Index(string(b), "example.com/a/register") > strings.Index(string(b), "example.com/z/register") {
		t.Fatalf("generated main = %s, %v", b, err)
	}
	if buildHash([]string{"b", "a"}) != buildHash([]string{"a", "b"}) || registerImport("example.com/p") != "example.com/p/register" {
		t.Fatal("build helper determinism mismatch")
	}
}
