package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestBuildModuleLocationAndSourceSelectionHelpers(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	local, err := buildDirFor(false)
	wantLocal := filepath.Join(currentDir, ".wago", "builds", managerVersion(), "standard", "normal")
	if err != nil || local != wantLocal {
		t.Fatalf("local buildDirFor = %q, %v", local, err)
	}
	global, err := buildDirFor(true)
	dirs := wago.DirsFor(managerVersion())
	wantGlobal := filepath.Join(dirs.Versions, managerVersion(), "standard", "normal", "plugins")
	if err != nil || global != wantGlobal {
		t.Fatalf("global buildDirFor = %q, %v; want %q", global, err, wantGlobal)
	}
	if source, err := depsSource(false); err != nil || source != "." {
		t.Fatalf("local depsSource = %q, %v", source, err)
	}
	if source, err := depsSource(true); err != nil || source != dirs.Data {
		t.Fatalf("global depsSource = %q, %v; want %q", source, err, dirs.Data)
	}
}

func TestPluginBuildTargetsActiveRuntime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WAGO_HOME", root)
	const currentManager = "manager-new"
	dirs := wago.DirsFor(currentManager)
	if err := managerversion.SetActiveInstallation(dirs, "canary-old", wagopaths.ProfileMinimal, wagopaths.BuildTiny); err != nil {
		t.Fatal(err)
	}
	oldVersion := configuredManagerVersion
	ConfigureManagerVersion(currentManager)
	t.Cleanup(func() { configuredManagerVersion = oldVersion })

	if got := pluginRuntimeVersion(); got != "canary-old" {
		t.Fatalf("plugin runtime version = %q, want canary-old", got)
	}
	if got := pluginBuildDefaultVariant(); got != "tiny" {
		t.Fatalf("plugin runtime build = %q, want tiny", got)
	}
	dir, err := localPluginBuildDir()
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := filepath.Join(".wago", "builds", "canary-old", "standard", "tiny")
	if !filepath.IsAbs(dir) || !strings.HasSuffix(filepath.Clean(dir), wantSuffix) {
		t.Fatalf("plugin build dir = %q, want suffix %q", dir, wantSuffix)
	}
}

func TestPluginOperationKeepsCapturedRuntime(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_RUNTIME_BUILD", "")
	t.Setenv("WAGO_RUNTIME_PROFILE", "")
	dirs := wagopaths.DirsFor(managerVersion())
	if err := managerversion.SetActiveInstallation(dirs, "runtime-a", wagopaths.ProfileMinimal, wagopaths.BuildTiny); err != nil {
		t.Fatal(err)
	}
	selection := capturePluginRuntime()
	if err := managerversion.SetActiveInstallation(dirs, "runtime-b", wagopaths.ProfileStandard, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}
	// Include environment overrides: both state sources belong to the operation.
	t.Setenv("WAGO_RUNTIME_BUILD", "normal")
	for _, global := range []bool{false, true} {
		dir, err := selection.buildDirFor(global)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join("runtime-a", "standard", "tiny")
		if !strings.Contains(dir, want) {
			t.Fatalf("captured build dir %q does not contain %q", dir, want)
		}
	}
	if config := selection.config(); config.RuntimeVersion != "runtime-a" || config.Profile != "standard" {
		t.Fatalf("build config changed during operation: %+v", config)
	}
	if next := capturePluginRuntime(); next.version != "runtime-b" || next.build != "normal" {
		t.Fatalf("next operation did not see new state: %+v", next)
	}
}
