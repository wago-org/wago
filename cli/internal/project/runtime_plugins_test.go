//go:build !wago_minimal

package project

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPluginsParsesCapabilitiesAndOrdering(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"plugins":{"wago-org/workers":"^0.0.0"}}`)
	writePluginLock(t, dir, LockEntry{
		Capabilities: json.RawMessage(`["instance.manage","runtime.lifecycle"]`),
		Config:       json.RawMessage(`{"maxWorkers":4}`),
	})
	got, err := PluginIntents(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantCapabilities := []string{"instance.manage", "runtime.lifecycle"}
	if len(got) != 1 || got[0].Name != "wago-org/workers" || !reflect.DeepEqual(got[0].Capabilities, wantCapabilities) {
		t.Fatalf("Plugins = %#v", got)
	}
	if !jsonEqual(got[0].Config, json.RawMessage(`{"maxWorkers":4}`)) {
		t.Fatalf("config = %s", got[0].Config)
	}
}

func TestPluginsParsesDocumentedVersionMap(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"plugins":{"wago-org/wasi":"^0.0.0"}}`)
	got, err := PluginIntents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "wago-org/wasi" {
		t.Fatalf("Plugins = %#v", got)
	}
}

func TestPluginsParsesCapabilityBudgets(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"plugins":{"wago-org/workers":"^0.0.0"}}`)
	writePluginLock(t, dir, LockEntry{
		Capabilities: json.RawMessage(`{"runtime.lifecycle":true,"instance.manage":{"maxInstances":3,"maxMemoryBytes":131072}}`),
	})
	got, err := PluginIntents(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantCapabilities := []string{"instance.manage", "runtime.lifecycle"}
	if !reflect.DeepEqual(got[0].Capabilities, wantCapabilities) {
		t.Fatalf("capabilities = %v", got[0].Capabilities)
	}
	want := CapabilityBudget{MaxInstances: 3, MaxMemoryBytes: 131072}
	if got[0].Budgets["instance.manage"] != want {
		t.Fatalf("budget = %#v", got[0].Budgets)
	}
}

func writePluginLock(t *testing.T, dir string, entry LockEntry) {
	t.Helper()
	if err := WriteLock(dir, LockDocument{Packages: map[string]LockEntry{"wago-org/workers": entry}}); err != nil {
		t.Fatal(err)
	}
}

func writeManifest(t *testing.T, dir, manifest string) {
	t.Helper()
	if err := os.WriteFile(Path(dir), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}
