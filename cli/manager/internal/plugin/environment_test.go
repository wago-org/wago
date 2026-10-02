package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	projectconfig "github.com/wago-org/wago/cli/internal/project"
)

func TestPluginEnvironmentUsesSharedGlobalIntentAndToolchainBuild(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	t.Setenv("WAGO_RUNTIME_PROFILE", "standard")
	t.Setenv("WAGO_RUNTIME_BUILD", "tiny")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	dirs := wago.DirsFor(managerVersion())
	if err := os.MkdirAll(dirs.Data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := projectconfig.AddDependency(dirs.Data, "github.com/wago-org/wasi", "^0.0.0"); err != nil {
		t.Fatal(err)
	}

	environment, err := resolvePluginEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if environment.scope != "global" || environment.manifestDir != dirs.Data {
		t.Fatalf("global environment = %+v", environment)
	}
	wantBuild := filepath.Join(dirs.Versions, managerVersion(), "standard", "tiny", "plugins")
	if environment.buildDir != wantBuild {
		t.Fatalf("global build dir = %q, want %q", environment.buildDir, wantBuild)
	}
}

func TestPluginEnvironmentKeepsLocalAndGlobalIsolated(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	dirs := wago.DirsFor(managerVersion())
	if err := os.MkdirAll(dirs.Data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := projectconfig.AddDependency(dirs.Data, "github.com/wago-org/wasi", "^0.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := projectconfig.Initialize("."); err != nil {
		t.Fatal(err)
	}

	environment, err := resolvePluginEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if environment.scope != "local" || len(environment.dependencies) != 0 {
		t.Fatalf("local environment merged global state: %+v", environment)
	}

	t.Setenv("WAGO_GLOBAL", "1")
	environment, err = resolvePluginEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if environment.scope != "global" || len(environment.dependencies) != 1 {
		t.Fatalf("explicit global environment = %+v", environment)
	}
}

func TestPluginEnvironmentExplainsMissingExplicitLocalManifest(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "1")
	t.Setenv("WAGO_BARE", "")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	_, err = resolvePluginEnvironment()
	if err == nil {
		t.Fatal("resolvePluginEnvironment succeeded without a local wago.json")
	}
	wantPath := projectconfig.DisplayPath(project)
	if got := err.Error(); !strings.Contains(got, wantPath) || !strings.Contains(got, "wago init") {
		t.Fatalf("error = %q, want path %q and wago init recovery", got, wantPath)
	}
}

func TestRunPluginScopeOverrides(t *testing.T) {
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyTestPluginScope(t, []string{"run", "--global", "module.wasm"})
	if !projectconfig.Truthy("WAGO_GLOBAL") || projectconfig.Truthy("WAGO_BARE") {
		t.Fatal("--global did not select global plugins")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyTestPluginScope(t, []string{"run", "-g", "module.wasm"})
	if !projectconfig.Truthy("WAGO_GLOBAL") {
		t.Fatal("-g did not select global plugins")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyTestPluginScope(t, []string{"run", "--bare", "module.wasm"})
	if !projectconfig.Truthy("WAGO_BARE") || projectconfig.Truthy("WAGO_GLOBAL") {
		t.Fatal("--bare did not disable plugins")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyTestPluginScope(t, []string{"run", "module.wasm", "--global"})
	if projectconfig.Truthy("WAGO_GLOBAL") {
		t.Fatal("guest argument after module path changed plugin scope")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyTestPluginScope(t, []string{"run", "--invoke", "_start", "--global", "module.wasm"})
	if !projectconfig.Truthy("WAGO_GLOBAL") {
		t.Fatal("value-taking run flag hid --global")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyTestPluginScope(t, []string{"run", "--local", "module.wasm"})
	if !projectconfig.Truthy("WAGO_LOCAL") || projectconfig.Truthy("WAGO_GLOBAL") || projectconfig.Truthy("WAGO_BARE") {
		t.Fatal("--local did not select project plugins")
	}
}

func TestPluginCommandScopeOverrides(t *testing.T) {
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyTestPluginScope(t, []string{"plugin", "list", "--global"})
	if !projectconfig.Truthy("WAGO_GLOBAL") {
		t.Fatal("plugin list --global did not select global plugins")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	applyTestPluginScope(t, []string{"plugins", "inspect", "github.com/wago-org/wasi", "--local"})
	if !projectconfig.Truthy("WAGO_LOCAL") {
		t.Fatal("plugins inspect --local did not select project plugins")
	}
}

func applyTestPluginScope(t *testing.T, args []string) {
	t.Helper()
	if err := ApplyScope(args, testEnvironment{}); err != nil {
		t.Fatal(err)
	}
}
