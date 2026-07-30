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
  "plugins": {"wago-org/workers": {
    "capabilities": ["instance.manage", "runtime.lifecycle"],
    "after": ["github.com/acme/wago-metrics"],
    "config": {"maxWorkers": 4}
  }}
}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := projectPlugins(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantCaps := []wago.PluginCapability{wago.PluginManagedInstances, wago.PluginRuntimeHooks}
	if len(got) != 1 || got[0].Name != "wago-org/workers" || !reflect.DeepEqual(got[0].Capabilities, wantCaps) || !reflect.DeepEqual(got[0].After, []string{"github.com/acme/wago-metrics"}) {
		t.Fatalf("projectPlugins = %#v", got)
	}
	if string(got[0].Config) != `{"maxWorkers":4}` {
		t.Fatalf("config = %s", got[0].Config)
	}
}

func TestProjectPluginsParsesCapabilityBudgets(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"plugins":{"wago-org/workers":{"capabilities":{"runtime.lifecycle":true,"instance.manage":{"maxInstances":3,"maxMemoryBytes":131072}}}}}`
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

func TestInitializeProjectCreatesMinimalManifestAndPreservesFields(t *testing.T) {
	dir := t.TempDir()
	created, err := initializeProject(dir)
	if err != nil || !created {
		t.Fatalf("initializeProject first = %v, %v", created, err)
	}
	m, err := readProjectMap(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m["$schema"] != manifestSchemaURI || m["schema"] != manifestVersion {
		t.Fatalf("initialized metadata = %#v", m)
	}
	if deps := depsFromMap(m); len(deps) != 0 {
		t.Fatalf("initialized dependencies = %v", deps)
	}
	m["name"] = "example"
	if err := writeProjectMap(dir, m); err != nil {
		t.Fatal(err)
	}
	created, err = initializeProject(dir)
	if err != nil || created {
		t.Fatalf("initializeProject repeat = %v, %v", created, err)
	}
	m, _ = readProjectMap(dir)
	if m["name"] != "example" {
		t.Fatalf("repeat initialization discarded fields: %#v", m)
	}
}
