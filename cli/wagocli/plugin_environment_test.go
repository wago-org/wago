//go:build !wago_manager && !wago_minimal

package wagocli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago"
)

func TestPluginEnvironmentUsesSharedGlobalIntentAndToolchainBuild(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	t.Setenv("WAGO_RUNTIME_PROFILE", "lite")
	t.Setenv("WAGO_RUNTIME_BUILD", "tiny")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	dirs := wago.DirsFor(versionString())
	if err := os.MkdirAll(dirs.Data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := addProjectDep(dirs.Data, "github.com/wago-org/wasi"); err != nil {
		t.Fatal(err)
	}

	environment, err := resolvePluginEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if environment.scope != "global" || environment.manifestDir != dirs.Data {
		t.Fatalf("global environment = %+v", environment)
	}
	wantBuild := filepath.Join(dirs.Versions, versionString(), "lite", "tiny", "plugins")
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

	dirs := wago.DirsFor(versionString())
	if err := os.MkdirAll(dirs.Data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := addProjectDep(dirs.Data, "github.com/wago-org/wasi"); err != nil {
		t.Fatal(err)
	}
	if _, err := initializeProject("."); err != nil {
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

func TestRunPluginScopeOverrides(t *testing.T) {
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyRunPluginScope([]string{"run", "--global", "module.wasm"})
	if !truthyEnv("WAGO_GLOBAL") || truthyEnv("WAGO_BARE") {
		t.Fatal("--global did not select global plugins")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyRunPluginScope([]string{"run", "--bare", "module.wasm"})
	if !truthyEnv("WAGO_BARE") || truthyEnv("WAGO_GLOBAL") {
		t.Fatal("--bare did not disable plugins")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyRunPluginScope([]string{"run", "module.wasm", "--global"})
	if truthyEnv("WAGO_GLOBAL") {
		t.Fatal("guest argument after module path changed plugin scope")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyRunPluginScope([]string{"run", "--invoke", "_start", "--global", "module.wasm"})
	if !truthyEnv("WAGO_GLOBAL") {
		t.Fatal("value-taking run flag hid --global")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyRunPluginScope([]string{"run", "--local", "module.wasm"})
	if !truthyEnv("WAGO_LOCAL") || truthyEnv("WAGO_GLOBAL") || truthyEnv("WAGO_BARE") {
		t.Fatal("--local did not select project plugins")
	}
}

func TestPluginCommandScopeOverrides(t *testing.T) {
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	t.Setenv("WAGO_BARE", "")
	applyInvocationPluginScope([]string{"plugin", "list", "--global"})
	if !truthyEnv("WAGO_GLOBAL") {
		t.Fatal("plugin list --global did not select global plugins")
	}

	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_LOCAL", "")
	applyInvocationPluginScope([]string{"plugins", "inspect", "wago-org/wasi", "--local"})
	if !truthyEnv("WAGO_LOCAL") {
		t.Fatal("plugins inspect --local did not select project plugins")
	}
}
