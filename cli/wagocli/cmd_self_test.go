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
	managed := filepath.Join(root, ".wago")
	project := filepath.Join(root, "project")
	manager := filepath.Join(managed, "bin", "wago")
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
	zshrc := filepath.Join(root, ".zshrc")
	pathBlock := "export KEEP=1\n\n# Wago PATH: " + filepath.Dir(manager) +
		"\nexport PATH='" + filepath.Dir(manager) + "':\"$PATH\"\n"
	if err := os.WriteFile(zshrc, []byte(pathBlock), 0o644); err != nil {
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
	shellConfig, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatalf("read shell config: %v", err)
	}
	if got, want := string(shellConfig), "export KEEP=1\n"; got != want {
		t.Fatalf("shell config after uninstall = %q, want %q", got, want)
	}
	if !strings.Contains(output.String(), "Uninstalled Wago") {
		t.Fatalf("uninstall output = %q", output.String())
	}
}

func TestRemoveInstallerPathBlocksPreservesUnrelatedLines(t *testing.T) {
	config := filepath.Join(t.TempDir(), ".zshrc")
	body := "# Wago PATH: /old/wago/bin\nexport KEEP=1\n"
	if err := os.WriteFile(config, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := removeInstallerPathBlocks(config); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if want := "export KEEP=1\n"; string(got) != want {
		t.Fatalf("shell config after orphan marker cleanup = %q, want %q", got, want)
	}
	info, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o640); got != want {
		t.Fatalf("shell config mode = %v, want %v", got, want)
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

func TestSelfUninstallTargetsCollapseCustomWagoHome(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "custom-wago")
	t.Setenv("HOME", home)
	t.Setenv("WAGO_HOME", root)
	dirs := wagopaths.Dirs{
		Data:   filepath.Join(root, "data"),
		Config: filepath.Join(root, "config"),
		Cache:  filepath.Join(root, "cache", "canary"),
	}
	manager := filepath.Join(root, "bin", "wago")
	if got, want := selfUninstallTargets(dirs, manager), []string{root}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selfUninstallTargets(custom home) = %q, want %q", got, want)
	}
}
