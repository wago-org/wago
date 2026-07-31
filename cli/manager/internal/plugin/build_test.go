package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/project"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestPluginRuntimeBinaryResolvesGlobalBuild(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "")
	t.Setenv("WAGO_GLOBAL", "")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	emptyProject := t.TempDir()
	if err := os.Chdir(emptyProject); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	buildDir, err := buildDirFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		t.Fatal(err)
	}
	manifestDir := sharedGlobalPluginDir(wago.DirsFor(managerVersion()))
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const plugin = "github.com/wago-org/wasi"
	if _, err := project.AddDependency(manifestDir, plugin, "^0.0.0"); err != nil {
		t.Fatal(err)
	}

	deps := []string{plugin}
	bin := pluginbuild.BinaryPath(buildDir)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("cached plugin runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin+".hash", []byte(pluginbuild.Hash(deps, pluginBuildConfig())), 0o644); err != nil {
		t.Fatal(err)
	}

	got, configured, err := pluginRuntimeBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !configured || got != bin {
		t.Fatalf("plugin runtime = %q, %v; want %q, true", got, configured, bin)
	}
}

type testEnvironment struct{}

func (testEnvironment) SelectScope(global, local, bare bool) error {
	return Select(global, local, bare)
}

func (testEnvironment) RuntimeBinary() (string, bool, error) {
	return RuntimeBinary()
}

func TestRuntimePathForInvocationLeavesMinimalRuntimeAlone(t *testing.T) {
	const base = "/runtime/wago"
	got, err := Resolve(base, wagopaths.ProfileMinimal, []string{"run", "module.wasm"}, testEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	if got != base {
		t.Fatalf("minimal runtime path = %q, want %q", got, base)
	}
}

func TestCapabilityReviewPinsVersionEvenWhenPluginCannotBeInspected(t *testing.T) {
	dir := t.TempDir()
	reviewInstalledCapabilities(dir, filepath.Join(dir, "missing-runtime"), "github.com/wago-org/wasi", "v1.2.3", pkgOpts{})

	lock, err := project.ReadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := lock.Packages["wago-org/wasi"].Version; got != "v1.2.3" {
		t.Fatalf("locked version = %q", got)
	}
}
