package wagocli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/wagopaths"
)

func TestSelfUpdateChannelPreservesReleaseTrack(t *testing.T) {
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
		if got := selfUpdateChannel(version); got != want {
			t.Errorf("selfUpdateChannel(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestSelfUninstallRequiresConfirmationAndPreservesProjects(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	managed := filepath.Join(root, "managed")
	project := filepath.Join(root, "project")
	manager := filepath.Join(root, "bin", "wago")
	for _, path := range []string{managed, project, filepath.Dir(manager)} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(manager, []byte("manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(project, projectFile)
	if err := os.WriteFile(manifest, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs := wagopaths.Dirs{
		Data:   managed,
		Config: filepath.Join(managed, "config"),
		Cache:  filepath.Join(managed, "cache", "test"),
	}

	var output bytes.Buffer
	selfUninstall(dirs, manager, false, strings.NewReader("\n"), &output)
	if _, err := os.Stat(manager); err != nil {
		t.Fatalf("cancelled uninstall removed manager: %v", err)
	}

	output.Reset()
	selfUninstall(dirs, manager, true, strings.NewReader(""), &output)
	if _, err := os.Stat(manager); !os.IsNotExist(err) {
		t.Fatalf("manager still exists after uninstall: %v", err)
	}
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("uninstall removed project manifest: %v", err)
	}
	if !strings.Contains(output.String(), "Uninstalled Wago") {
		t.Fatalf("uninstall output = %q", output.String())
	}
}

func TestSelfUninstallTargetsOnlyManagedState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dirs := wagopaths.Dirs{
		Data:    filepath.Join(home, ".wago"),
		Config:  filepath.Join(home, ".wago", "config"),
		Cache:   filepath.Join(home, ".wago", "cache", "canary"),
		Version: "canary",
	}
	manager := filepath.Join(home, ".local", "bin", "wago")
	got := selfUninstallTargets(dirs, manager)
	want := []string{filepath.Join(home, ".wago"), manager}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selfUninstallTargets() = %q, want %q", got, want)
	}
}
