//go:build !wago_minimal

package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/wagopaths"
)

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

	manifestDir := wagopaths.DirsFor("runtime").Data
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"plugins":{"wago-org/wasi":"^0.0.0"}}`
	if err := os.WriteFile(filepath.Join(manifestDir, project.File), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteLock(manifestDir, project.LockDocument{Packages: map[string]project.LockEntry{
		"wago-org/wasi": {Capabilities: json.RawMessage(`["wasi"]`)},
	}}); err != nil {
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
