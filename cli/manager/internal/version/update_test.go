package version

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestInstalledCommitMatchesOnlyCanonicalHashes(t *testing.T) {
	runtime := filepath.Join(t.TempDir(), "wago-runtime")
	if err := os.WriteFile(runtime, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := runtimeVersionOutput
	t.Cleanup(func() { runtimeVersionOutput = previous })
	runtimeVersionOutput = func(string) ([]byte, error) {
		return []byte("Wago\n  release      canary-deadbee\n"), nil
	}
	if installedCommitMatches(runtime, "canary@deadbee123456789012345678901234567890123") {
		t.Fatal("legacy abbreviated canary hash was treated as canonical")
	}
	if installedCommitMatches(runtime, "canary@cafef00123456789012345678901234567890123") {
		t.Fatal("different canary hashes matched")
	}
	runtimeVersionOutput = func(string) ([]byte, error) {
		return []byte("Wago\n  release      canary@DEADBEE123456789012345678901234567890123\n"), nil
	}
	if !installedCommitMatches(runtime, "canary@deadbee123456789012345678901234567890123") {
		t.Fatal("equal canonical canary hashes did not match")
	}
}

func TestSameReleaseRejectsMalformedRollingIdentityEvenWhenTextMatches(t *testing.T) {
	for _, version := range []string{
		"canary@deadbee",
		"nightly@deadbee12345678901234567890123456789012z",
		"canary",
	} {
		if sameRelease(version, version) {
			t.Errorf("sameRelease(%q, %q) accepted malformed or moving rolling identity", version, version)
		}
	}
	if !sameRelease("v1.2.3", "v1.2.3") {
		t.Fatal("equal stable releases did not match")
	}
}

func TestRollingCommitIdentityRequiresCanonicalSHA(t *testing.T) {
	for _, test := range []struct {
		version, channel, sha string
		canonical             bool
	}{
		{"canary@deadbee123456789012345678901234567890123", "canary", "deadbee123456789012345678901234567890123", true},
		{"nightly-20260731-cafef00@cafef00123456789012345678901234567890123", "nightly", "cafef00123456789012345678901234567890123", true},
		{" CANARY@DEADBEE123456789012345678901234567890123 ", "canary", "deadbee123456789012345678901234567890123", true},
		{"canary@deadbee123456789012345678901234567890123@junk", "", "", false},
		{"canary-deadbee", "", "", false},
		{"nightly-20260731-cafef00", "", "", false},
		{"v0.2.0", "", "", false},
	} {
		channel, sha, canonical := rollingCommitSHA(test.version)
		if channel != test.channel || sha != test.sha || canonical != test.canonical {
			t.Errorf("rollingCommitSHA(%q) = %q, %q, %v", test.version, channel, sha, canonical)
		}
	}
	canonical := " NIGHTLY-20260731-CAFEF00@CAFEF00123456789012345678901234567890123 "
	if got := releaseAssetVersion(canonical); got != "nightly-20260731-cafef00" {
		t.Fatalf("releaseAssetVersion(%q) = %q", canonical, got)
	}
	if got := releasePickerLabel(canonical); got != "nightly-cafef00" {
		t.Fatalf("releasePickerLabel(%q) = %q", canonical, got)
	}
}

func TestVersionUpdateSkipsMatchingInstalledCommitUnlessForced(t *testing.T) {
	root := t.TempDir()
	dirs := wagopaths.Dirs{
		Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"),
		Versions: filepath.Join(root, "versions"), Cache: filepath.Join(root, "cache"), Version: "canary",
	}
	dest := dirs.RuntimeBinary("canary", string(wagopaths.ProfileStandard), string(wagopaths.BuildNormal))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldResolve, oldInstall, oldOutput := resolveUpdateRunnerVersionContext, installUpdateRunnerPayloadContext, runtimeVersionOutput
	t.Cleanup(func() {
		resolveUpdateRunnerVersionContext, installUpdateRunnerPayloadContext, runtimeVersionOutput = oldResolve, oldInstall, oldOutput
	})
	resolveUpdateRunnerVersionContext = func(context.Context, string, *managerprogress.Progress) (string, bool, error) {
		return "canary@deadbee123456789012345678901234567890123", false, nil
	}
	runtimeVersionOutput = func(string) ([]byte, error) {
		return []byte("Wago\n  release      canary@deadbee123456789012345678901234567890123\n"), nil
	}
	installs := 0
	installUpdateRunnerPayloadContext = func(context.Context, string, wagopaths.Profile, wagopaths.Build, string, bool, *managerprogress.Progress) error {
		installs++
		return nil
	}

	vmUpdate(dirs, "canary", wagopaths.ProfileStandard, wagopaths.BuildNormal, "no", false)
	if installs != 0 {
		t.Fatalf("matching update installed %d payloads", installs)
	}
	vmUpdate(dirs, "canary", wagopaths.ProfileStandard, wagopaths.BuildNormal, "no", true)
	if installs != 1 {
		t.Fatalf("forced update installed %d payloads, want 1", installs)
	}
}
