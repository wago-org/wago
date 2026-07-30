package wagocli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wago-org/wago"
)

func TestProjectPluginsParsesCapabilitiesAndOrdering(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  "plugins": {
    "wago-org/workers": "^0.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := lockDoc{Packages: map[string]lockEntry{
		"wago-org/workers": {
			Version:              "0.0.0",
			RequiredCapabilities: []string{"instance.manage", "runtime.lifecycle"},
			Capabilities:         json.RawMessage(`["instance.manage","runtime.lifecycle"]`),
			Config:               json.RawMessage(`{"maxWorkers":4}`),
		},
	}}
	if err := writeLock(dir, lock); err != nil {
		t.Fatal(err)
	}
	got, err := projectPlugins(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantCaps := []wago.PluginCapability{wago.PluginManagedInstances, wago.PluginRuntimeHooks}
	if len(got) != 1 || got[0].Name != "wago-org/workers" || !reflect.DeepEqual(got[0].Capabilities, wantCaps) {
		t.Fatalf("projectPlugins = %#v", got)
	}
	if !json.Valid(got[0].Config) || !reflect.DeepEqual(decodeJSON(t, got[0].Config), map[string]any{"maxWorkers": float64(4)}) {
		t.Fatalf("config = %s", got[0].Config)
	}
}

func decodeJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestProjectPluginsParsesDocumentedVersionMap(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  "plugins": {
    "wago-org/wasi": "^0.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := projectPlugins(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "wago-org/wasi" {
		t.Fatalf("projectPlugins = %#v", got)
	}
}

func TestProjectPluginsRejectsLegacyManifestShape(t *testing.T) {
	for _, manifest := range []string{
		`{"schema":"wago/other","plugins":[]}`,
		`{"dependencies":["github.com/wago-org/wasi"],"plugins":{"wago-org/wasi":"^0.0.0"}}`,
		`{"plugins":[{"name":"wago-org/wasi","capabilities":[]}]}`,
		`{"plugins":{"wago-org/wasi":"newest"}}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := projectPlugins(dir); err == nil {
			t.Fatalf("projectPlugins accepted legacy/invalid manifest: %s", manifest)
		}
	}
}

func TestProjectPluginsParsesCapabilityBudgets(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"plugins":{"wago-org/workers":"^0.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := lockDoc{Packages: map[string]lockEntry{
		"wago-org/workers": {
			Capabilities: json.RawMessage(`{"runtime.lifecycle":true,"instance.manage":{"maxInstances":3,"maxMemoryBytes":131072}}`),
		},
	}}
	if err := writeLock(dir, lock); err != nil {
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
	if m["$schema"] != manifestSchemaURI {
		t.Fatalf("initialized metadata = %#v", m)
	}
	if _, exists := m["schema"]; exists {
		t.Fatalf("initialized manifest contains removed schema version: %#v", m)
	}
	if deps, err := projectDeps(dir); err != nil || len(deps) != 0 {
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
