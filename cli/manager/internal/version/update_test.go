package version

import (
	"os"
	"path/filepath"
	"testing"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestInstalledCommitMatchesShortAndFullHashes(t *testing.T) {
	runtime := filepath.Join(t.TempDir(), "wago-runtime")
	if err := os.WriteFile(runtime, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := runtimeVersionOutput
	t.Cleanup(func() { runtimeVersionOutput = previous })
	runtimeVersionOutput = func(string) ([]byte, error) {
		return []byte("Wago\n  release      canary-deadbee\n"), nil
	}
	if !installedCommitMatches(runtime, "canary@deadbee123456789012345678901234567890123") {
		t.Fatal("matching short and full canary hashes were not recognized")
	}
	if installedCommitMatches(runtime, "canary@cafef00123456789012345678901234567890123") {
		t.Fatal("different canary hashes matched")
	}
}

func TestCommitFromVersionUnderstandsRollingReleaseNames(t *testing.T) {
	for _, test := range []struct{ version, want string }{
		{"canary@deadbee123456789012345678901234567890123", "deadbee123456789012345678901234567890123"},
		{"canary-deadbee", "deadbee"},
		{"nightly-20260731-cafef00", "cafef00"},
		{"v0.2.0", ""},
	} {
		if got := commitFromVersion(test.version); got != test.want {
			t.Errorf("commitFromVersion(%q) = %q, want %q", test.version, got, test.want)
		}
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
	oldResolve, oldInstall, oldOutput := resolveUpdateRunnerVersion, installUpdateRunnerPayload, runtimeVersionOutput
	t.Cleanup(func() {
		resolveUpdateRunnerVersion, installUpdateRunnerPayload, runtimeVersionOutput = oldResolve, oldInstall, oldOutput
	})
	resolveUpdateRunnerVersion = func(string, *managerprogress.Progress) (string, bool, error) {
		return "canary@deadbee123456789012345678901234567890123", false, nil
	}
	runtimeVersionOutput = func(string) ([]byte, error) {
		return []byte("Wago\n  release      canary-deadbee\n"), nil
	}
	installs := 0
	installUpdateRunnerPayload = func(string, wagopaths.Profile, wagopaths.Build, string, bool, *managerprogress.Progress) error {
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
