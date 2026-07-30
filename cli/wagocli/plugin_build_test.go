//go:build !wago_manager && !wago_minimal

package wagocli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wago-org/wago"
)

func TestPluginListHandsOffToGlobalPluginRuntime(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "")
	t.Setenv("WAGO_GLOBAL", "")
	t.Setenv("WAGO_PLUGIN_ACTIVE", "")

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
	const plugin = "github.com/wago-org/wasi"
	if _, err := addProjectDep(buildDir, plugin); err != nil {
		t.Fatal(err)
	}

	deps := []string{plugin}
	bin := filepath.Join(buildDir, "bin", "wago"+exeSuffix())
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("cached plugin runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin+".hash", []byte(buildHash(deps)), 0o644); err != nil {
		t.Fatal(err)
	}

	oldArgs := os.Args
	os.Args = []string{"wago", "plugin", "list"}
	t.Cleanup(func() { os.Args = oldArgs })

	called := false
	oldHandoff := handoffPluginProcess
	handoffPluginProcess = func(gotBin string, gotArgs, gotEnv []string) error {
		called = true
		if gotBin != bin {
			t.Errorf("handoff binary = %q, want %q", gotBin, bin)
		}
		if !slices.Equal(gotArgs, []string{bin, "plugin", "list"}) {
			t.Errorf("handoff args = %q", gotArgs)
		}
		if !slices.Contains(gotEnv, "WAGO_PLUGIN_ACTIVE="+buildHash(deps)) {
			t.Errorf("handoff environment lacks WAGO_PLUGIN_ACTIVE")
		}
		return nil
	}
	t.Cleanup(func() { handoffPluginProcess = oldHandoff })

	prepareRunnerInvocation([]string{"plugin", "list"})
	if !called {
		t.Fatal("plugin list did not hand off to the rebuilt global plugin runtime")
	}
}

func TestRunLoadsGlobalPluginConfiguration(t *testing.T) {
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
	const plugin = "github.com/wago-org/wasi"
	if _, err := addProjectDep(buildDir, plugin); err != nil {
		t.Fatal(err)
	}
	if err := setPluginGrants(buildDir, "wago-org/wasi", []string{"wasi"}); err != nil {
		t.Fatal(err)
	}

	configs, err := activePluginConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0].Name != "wago-org/wasi" {
		t.Fatalf("active plugin configs = %#v, want global wago-org/wasi", configs)
	}
	if !slices.Equal(configs[0].Capabilities, []wago.PluginCapability{"wasi"}) {
		t.Fatalf("global plugin capabilities = %#v, want wasi", configs[0].Capabilities)
	}
}
