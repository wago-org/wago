package self

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	projectconfig "github.com/wago-org/wago/cli/internal/project"
	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/managedrelease"
	"github.com/wago-org/wago/internal/wagopaths"
)

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
}

func TestSelfUpdateStagesAreUniqueAndSameDirectory(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "wago")
	if err := os.WriteFile(executable, []byte("manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := createSelfUpdateStage(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(first)
	second, err := createSelfUpdateStage(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(second)
	if first == second {
		t.Fatalf("self-update stages collided at %q", first)
	}
	for _, staged := range []string{first, second} {
		if filepath.Dir(staged) != directory {
			t.Fatalf("self-update stage %q is not beside executable", staged)
		}
	}
}

func TestSelfUpdateSkipsMatchingManagerCommit(t *testing.T) {
	fakeReleaseVerification(t)
	executable := filepath.Join(t.TempDir(), "wago")
	if err := os.WriteFile(executable, []byte("manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldResolve, oldInstall := resolveManagerUpdate, installManagerPayload
	t.Cleanup(func() {
		resolveManagerUpdate, installManagerPayload = oldResolve, oldInstall
	})
	resolveManagerUpdate = func(string, *managerprogress.Progress) (string, bool, error) {
		return "canary@2ff00ddd12345678901234567890123456789012", false, nil
	}
	installs := 0
	installManagerPayload = func(_ string, dest string, _ bool, _ *managerprogress.Progress) error {
		installs++
		return os.WriteFile(dest, []byte("updated manager"), 0o755)
	}

	current := "canary@2ff00ddd12345678901234567890123456789012"
	selfUpdate(current, executable, false)
	if installs != 0 {
		t.Fatalf("matching manager update installed %d payloads, want 0", installs)
	}
	selfUpdate(current, executable, true)
	if installs != 1 {
		t.Fatalf("forced manager update installed %d payloads, want 1", installs)
	}
}

func TestSelfUpdateSynchronizesManagedPluginBuildSource(t *testing.T) {
	fakeReleaseVerification(t)
	home := t.TempDir()
	setTestHome(t, home)
	managed := filepath.Join(home, ".wago")
	executable := filepath.Join(managed, "bin", "wago")
	source := filepath.Join(managed, "src")
	for _, dir := range []string{filepath.Dir(executable), source} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(executable, []byte("manager"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldResolve, oldInstall, oldSync := resolveManagerUpdate, installManagerPayload, syncManagerSource
	t.Cleanup(func() {
		resolveManagerUpdate, installManagerPayload, syncManagerSource = oldResolve, oldInstall, oldSync
	})
	const resolved = "canary@2ff00ddd12345678901234567890123456789012"
	resolveManagerUpdate = func(string, *managerprogress.Progress) (string, bool, error) {
		return resolved, true, nil
	}
	installManagerPayload = func(_ string, dest string, _ bool, _ *managerprogress.Progress) error {
		return os.WriteFile(dest, []byte("updated manager"), 0o755)
	}
	var syncedRef, syncedDest string
	syncManagerSource = func(ref, dest string, _ *managerprogress.Progress) error {
		syncedRef, syncedDest = ref, dest
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, "go.mod"), []byte("module github.com/wago-org/wago\n"), 0644)
	}

	selfUpdate("canary@old", executable, true)
	selected, err := managedrelease.SelectedBinary(executable)
	if err != nil {
		t.Fatal(err)
	}
	if syncedRef != resolved || syncedDest != managedrelease.SourceForExecutable(selected) || syncedDest == source {
		t.Fatalf("source was not paired with selected executable: %q, %q", syncedRef, syncedDest)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("legacy source was removed", err)
	}
}

func TestSelfUninstallModePickerUsesRadioButtonsAndDefaultsFull(t *testing.T) {
	p := selfUninstallModePicker()
	if got := p.Selected(); got != string(Full) {
		t.Fatalf("default uninstall mode = %q, want %q", got, Full)
	}
	frame := p.Frame()
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
	if got := p.Selected(); got != "yes" {
		t.Fatalf("default uninstall confirmation = %q, want yes", got)
	}
	frame := p.Frame()
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
		mode, err := ParseMode(value)
		if err != nil || string(mode) != value {
			t.Fatalf("parseSelfUninstallMode(%q) = %q, %v", value, mode, err)
		}
	}
	if _, err := ParseMode("everything"); err == nil {
		t.Fatal("unknown uninstall mode was accepted")
	}
}

func TestSelfUninstallRequiresConfirmationAndPreservesProjects(t *testing.T) {
	root := t.TempDir()
	setTestHome(t, root)
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
	manifest := filepath.Join(project, projectconfig.File)
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
	selfUninstall(dirs, manager, Full, false, strings.NewReader("n\n"), &output)
	if _, err := os.Stat(manager); err != nil {
		t.Fatalf("cancelled uninstall removed manager: %v", err)
	}

	output.Reset()
	selfUninstall(dirs, manager, Full, true, strings.NewReader(""), &output)
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
	body := "# Wago PATH: /old/wago/bin\nexport KEEP=1\n\n# Wago completions\n. '/old/.wago/completions/wago.zsh'\n"
	if err := os.WriteFile(config, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := RemoveInstallerPathBlocks(config); err != nil {
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
	if got, want := info.Mode().Perm(), os.FileMode(0o640); runtime.GOOS != "windows" && got != want {
		t.Fatalf("shell config mode = %v, want %v", got, want)
	}
}

func TestSelfUninstallTargetsOnlyManagedState(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	dirs := wagopaths.Dirs{
		Data:    filepath.Join(home, ".wago"),
		Config:  filepath.Join(home, ".wago", "config"),
		Cache:   filepath.Join(home, ".wago", "cache", "canary"),
		Version: "canary",
	}
	manager := filepath.Join(home, ".local", "bin", "wago")
	got := Targets(dirs, manager, Full)
	want := []string{filepath.Join(home, ".wago"), manager}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selfUninstallTargets() = %q, want %q", got, want)
	}
}

func TestSelfUninstallRemovesInstalledFishCompletion(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", "")
	completion := filepath.Join(home, ".config", "fish", "completions", "wago.fish")
	if err := os.MkdirAll(filepath.Dir(completion), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(completion, []byte("completion"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs := wagopaths.Dirs{Data: filepath.Join(home, ".wago"), Cache: filepath.Join(home, ".wago", "cache", "canary")}
	targets := Targets(dirs, filepath.Join(home, ".wago", "bin", "wago"), Minimal)
	if !slices.Contains(targets, completion) {
		t.Fatalf("targets = %q, missing fish completion", targets)
	}
}

func TestSelfUninstallTargetsCollapseCustomWagoHome(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "custom-wago")
	setTestHome(t, home)
	t.Setenv("WAGO_HOME", root)
	dirs := wagopaths.Dirs{
		Data:   filepath.Join(root, "data"),
		Config: filepath.Join(root, "config"),
		Cache:  filepath.Join(root, "cache", "canary"),
	}
	manager := filepath.Join(root, "bin", "wago")
	if got, want := Targets(dirs, manager, Full), []string{root}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selfUninstallTargets(custom home) = %q, want %q", got, want)
	}
}

func TestSelfUninstallFullRemovesSelectedWagoHomeIncludingPlugins(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "selected-wago")
	setTestHome(t, home)
	t.Setenv("WAGO_HOME", root)
	dirs := wagopaths.DirsFor("canary")
	manager := filepath.Join(root, "bin", "wago")
	for path, body := range map[string]string{
		manager: "manager",
		filepath.Join(dirs.Data, projectconfig.File): "plugin manifest",
		filepath.Join(dirs.Data, "wago-lock.json"):   "plugin lock",
		filepath.Join(dirs.Data, "builds", "plugin"): "plugin runtime",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	selfUninstall(dirs, manager, Full, true, strings.NewReader(""), io.Discard)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("full uninstall kept selected Wago home %s: %v", root, err)
	}
}

func TestSelfUninstallFullRemovesCustomInstallationDirectory(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "state")
	installDir := filepath.Join(home, "custom-bin")
	setTestHome(t, home)
	t.Setenv("WAGO_HOME", root)
	dirs := wagopaths.DirsFor("canary")
	manager := filepath.Join(installDir, "wago")
	for path, body := range map[string]string{
		manager:                         "manager",
		filepath.Join(root, "leftover"): "state",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	selfUninstall(dirs, manager, Full, true, strings.NewReader(""), io.Discard)
	for _, path := range []string{root, installDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("full uninstall kept %s: %v", path, err)
		}
	}
}

func TestSelfUninstallFullPreservesSharedInstallationDirectory(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "state")
	installDir := filepath.Join(home, "shared-bin")
	setTestHome(t, home)
	t.Setenv("WAGO_HOME", root)
	dirs := wagopaths.DirsFor("canary")
	manager := filepath.Join(installDir, "wago")
	sibling := filepath.Join(installDir, "another-command")
	for _, path := range []string{manager, sibling, filepath.Join(root, "leftover")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	selfUninstall(dirs, manager, Full, true, strings.NewReader(""), io.Discard)
	if _, err := os.Stat(manager); !os.IsNotExist(err) {
		t.Fatalf("full uninstall kept manager %s: %v", manager, err)
	}
	if body, err := os.ReadFile(sibling); err != nil || string(body) != "another-command" {
		t.Fatalf("full uninstall changed shared install directory sibling: %q, %v", body, err)
	}
}

func TestSelfUninstallPartialPreservesGlobalPlugins(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
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
		filepath.Join(root, projectconfig.File):                "global plugins",
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

	selfUninstall(dirs, manager, Partial, true, strings.NewReader(""), io.Discard)

	for _, path := range []string{
		filepath.Join(root, projectconfig.File),
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
	setTestHome(t, home)
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

	selfUninstall(dirs, manager, Minimal, true, strings.NewReader(""), io.Discard)

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

func fakeReleaseVerification(t *testing.T) {
	t.Helper()
	oldVerify, oldSync := verifyManagerRelease, syncManagerSource
	t.Cleanup(func() { verifyManagerRelease, syncManagerSource = oldVerify, oldSync })
	verifyManagerRelease = func(string) error { return nil }
	syncManagerSource = func(_ string, dest string, _ *managerprogress.Progress) error {
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, "go.mod"), []byte("module github.com/wago-org/wago\n"), 0644)
	}
}
