package wagocli

import (
	"bytes"
	"io"
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

func TestSelfUpdateRuntimeTargetFollowsActiveRollingChannel(t *testing.T) {
	tests := []struct {
		active   string
		channel  string
		resolved string
		want     string
	}{
		{"canary", "canary", "canary@880e153000000000000000000000000000000000", "canary-880e153"},
		{"canary-6342d5e", "canary", "canary@880e153000000000000000000000000000000000", "canary-880e153"},
		{"nightly-20260729-6342d5e", "nightly", "nightly-20260730-880e153", "nightly-20260730-880e153"},
		{"nightly", "canary", "canary@880e153000000000000000000000000000000000", ""},
		{"v0.2.0", "latest", "v0.2.1", ""},
	}
	for _, test := range tests {
		if got := selfUpdateRuntimeTarget(test.active, test.channel, test.resolved); got != test.want {
			t.Errorf("selfUpdateRuntimeTarget(%q, %q, %q) = %q, want %q", test.active, test.channel, test.resolved, got, test.want)
		}
	}
}

func TestSelfUpdateRefreshesAndSelectsActiveRollingRuntime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	dirs := wagopaths.Dirs{
		Config:   filepath.Join(root, ".wago", "config"),
		Data:     filepath.Join(root, ".wago"),
		Versions: filepath.Join(root, ".wago", "versions"),
		Cache:    filepath.Join(root, ".wago", "cache", "canary-old"),
		Version:  "canary-old",
	}
	if err := setActiveInstallation(dirs, "canary-6342d5e", wagopaths.ProfileStandard, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}

	oldInstall, oldSync := installSelfRuntime, syncSelfSource
	t.Cleanup(func() {
		installSelfRuntime = oldInstall
		syncSelfSource = oldSync
	})
	var installedRef, syncedRef, syncedDest string
	installSelfRuntime = func(ref string, _ wagopaths.Profile, _ wagopaths.Build, dest string, _ bool, _ *installProgress) error {
		installedRef = ref
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, []byte("runtime"), 0o755)
	}
	syncSelfSource = func(ref, dest string, _ *installProgress) error {
		syncedRef, syncedDest = ref, dest
		return nil
	}

	const resolved = "canary@880e153000000000000000000000000000000000"
	updated, err := updateActiveRuntimeForSelf(dirs, "canary", resolved, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("active canary runtime was not updated")
	}
	if installedRef != resolved || syncedRef != resolved {
		t.Fatalf("updated refs = runtime %q, source %q; want %q", installedRef, syncedRef, resolved)
	}
	if want := filepath.Join(root, ".wago", "src"); syncedDest != want {
		t.Fatalf("source destination = %q, want %q", syncedDest, want)
	}
	if got := activeVersion(dirs); got != "canary-880e153" {
		t.Fatalf("active version = %q, want canary-880e153", got)
	}
	if _, _, _, ok := installedRuntime(dirs, "canary-880e153", wagopaths.ProfileStandard, wagopaths.BuildNormal); !ok {
		t.Fatal("updated runtime was not installed")
	}
}

func TestSelfUninstallModePickerUsesRadioButtonsAndDefaultsFull(t *testing.T) {
	p := selfUninstallModePicker()
	if got := p.selected(); got != string(selfUninstallFull) {
		t.Fatalf("default uninstall mode = %q, want %q", got, selfUninstallFull)
	}
	frame := p.frame()
	for _, want := range []string{
		"Choose uninstall mode",
		"› ◉ Full",
		"○ Partial",
		"○ Minimal",
		"including plugins",
		"keep global plugins",
		"Wago command and PATH only",
		"enter/→ select",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("uninstall picker missing %q:\n%s", want, frame)
		}
	}
}

func TestSelfUninstallConfirmationUsesRadioButtonsAndDefaultsYes(t *testing.T) {
	p := selfUninstallConfirmationPicker()
	if got := p.selected(); got != "yes" {
		t.Fatalf("default uninstall confirmation = %q, want yes", got)
	}
	frame := p.frame()
	for _, want := range []string{
		"Continue?",
		"› ◉ Yes",
		"○ No",
		"enter/→ select",
		"esc cancel",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("uninstall confirmation missing %q:\n%s", want, frame)
		}
	}
}

func TestSelfUninstallConfirmationDefaultsYesWithoutTTY(t *testing.T) {
	var output bytes.Buffer
	if !confirmSelfUninstallInteractive(strings.NewReader("\n"), &output, false) {
		t.Fatal("empty uninstall confirmation did not default to yes")
	}
	if !strings.Contains(output.String(), "Continue? [Y/n]") {
		t.Fatalf("non-interactive uninstall confirmation = %q", output.String())
	}
}

func TestParseSelfUninstallMode(t *testing.T) {
	for _, value := range []string{"full", "partial", "minimal"} {
		mode, err := parseSelfUninstallMode(value)
		if err != nil || string(mode) != value {
			t.Fatalf("parseSelfUninstallMode(%q) = %q, %v", value, mode, err)
		}
	}
	if _, err := parseSelfUninstallMode("everything"); err == nil {
		t.Fatal("unknown uninstall mode was accepted")
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
	selfUninstall(dirs, manager, selfUninstallFull, false, strings.NewReader("n\n"), &output)
	if _, err := os.Stat(manager); err != nil {
		t.Fatalf("cancelled uninstall removed manager: %v", err)
	}

	output.Reset()
	selfUninstall(dirs, manager, selfUninstallFull, true, strings.NewReader(""), &output)
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
	got := selfUninstallTargets(dirs, manager, selfUninstallFull)
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
	if got, want := selfUninstallTargets(dirs, manager, selfUninstallFull), []string{root}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selfUninstallTargets(custom home) = %q, want %q", got, want)
	}
}

func TestSelfUninstallPartialPreservesGlobalPlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".wago")
	dirs := wagopaths.Dirs{
		Data:     root,
		Config:   filepath.Join(root, "config"),
		Versions: filepath.Join(root, "versions"),
		Cache:    filepath.Join(root, "cache", "canary"),
	}
	manager := filepath.Join(root, "bin", "wago")
	for _, path := range []string{
		filepath.Join(dirs.Versions, "canary"),
		dirs.Config,
		dirs.Cache,
		filepath.Join(root, "src"),
		filepath.Dir(manager),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(root, versionPluginManifest):             "global plugins",
		filepath.Join(root, "wago-lock.json"):                  "global lock",
		filepath.Join(dirs.Config, "active-version"):           "canary",
		filepath.Join(dirs.Versions, "canary", "wago-runtime"): "runtime",
		filepath.Join(root, "src", "go.mod"):                   "source",
		manager:                                                "manager",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	selfUninstall(dirs, manager, selfUninstallPartial, true, strings.NewReader(""), io.Discard)

	for _, path := range []string{
		filepath.Join(root, versionPluginManifest),
		filepath.Join(root, "wago-lock.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("partial uninstall removed %s: %v", path, err)
		}
	}
	for _, path := range []string{manager, dirs.Versions, dirs.Config, filepath.Dir(dirs.Cache), filepath.Join(root, "src")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("partial uninstall kept %s: %v", path, err)
		}
	}
}

func TestSelfUninstallMinimalRemovesOnlyManager(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".wago")
	dirs := wagopaths.Dirs{
		Data:     root,
		Config:   filepath.Join(root, "config"),
		Versions: filepath.Join(root, "versions"),
		Cache:    filepath.Join(root, "cache", "canary"),
	}
	manager := filepath.Join(root, "bin", "wago")
	runtime := filepath.Join(dirs.Versions, "canary", "wago-runtime")
	for _, path := range []string{filepath.Dir(manager), filepath.Dir(runtime)} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{manager, runtime} {
		if err := os.WriteFile(path, []byte(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	zshrc := filepath.Join(home, ".zshrc")
	pathBlock := "# Wago PATH: " + filepath.Dir(manager) +
		"\nexport PATH='" + filepath.Dir(manager) + "':\"$PATH\"\n"
	if err := os.WriteFile(zshrc, []byte(pathBlock), 0o644); err != nil {
		t.Fatal(err)
	}

	selfUninstall(dirs, manager, selfUninstallMinimal, true, strings.NewReader(""), io.Discard)

	if _, err := os.Stat(manager); !os.IsNotExist(err) {
		t.Fatalf("minimal uninstall kept manager: %v", err)
	}
	if _, err := os.Stat(runtime); err != nil {
		t.Fatalf("minimal uninstall removed runtime: %v", err)
	}
	if config, err := os.ReadFile(zshrc); err != nil || len(config) != 0 {
		t.Fatalf("minimal uninstall PATH cleanup = %q, %v", config, err)
	}
}
