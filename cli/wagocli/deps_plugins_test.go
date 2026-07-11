package wagocli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wago-org/wago"
)

func TestProjectPluginsParsesCapabilitiesAndOrdering(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  "dependencies": ["github.com/wago-org/workers"],
  "plugins": [{
    "name": "github.com/wago-org/workers",
    "capabilities": ["instance.manage", "runtime.lifecycle"],
    "after": ["github.com/acme/wago-metrics"],
    "config": {"maxWorkers": 4}
  }]
}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := projectPlugins(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantCaps := []wago.PluginCapability{wago.PluginManagedInstances, wago.PluginRuntimeHooks}
	if len(got) != 1 || got[0].Name != "github.com/wago-org/workers" || !reflect.DeepEqual(got[0].Capabilities, wantCaps) || !reflect.DeepEqual(got[0].After, []string{"github.com/acme/wago-metrics"}) {
		t.Fatalf("projectPlugins = %#v", got)
	}
	if string(got[0].Config) != `{"maxWorkers":4}` {
		t.Fatalf("config = %s", got[0].Config)
	}
}

func TestProjectPluginsParsesCapabilityBudgets(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"plugins":[{"name":"github.com/wago-org/workers","capabilities":{"runtime.lifecycle":true,"instance.manage":{"maxInstances":3,"maxMemoryBytes":131072}}}]}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := projectPlugins(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantCaps := []wago.PluginCapability{wago.PluginManagedInstances, wago.PluginRuntimeHooks}
	if !reflect.DeepEqual(got[0].Capabilities, wantCaps) {
		t.Fatalf("capabilities = %v", got[0].Capabilities)
	}
	want := wago.CapabilityBudget{MaxInstances: 3, MaxMemoryBytes: 131072}
	if got[0].Budgets[wago.PluginManagedInstances] != want {
		t.Fatalf("budget = %#v", got[0].Budgets)
	}
}
