package wagocli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPluginGrantsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	seed := `{"plugins":{"wago-org/wasi":"^0.0.0","wago-org/new":"^0.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := lockDoc{Packages: map[string]lockEntry{
		"wago-org/wasi": {
			Version:      "0.0.0",
			Capabilities: json.RawMessage(`["wasi:stdio"]`),
			Config:       json.RawMessage(`{"dir":"/tmp"}`),
		},
	}}
	if err := writeLock(dir, lock); err != nil {
		t.Fatal(err)
	}

	if got := pluginGrants(dir, "wago-org/wasi"); !reflect.DeepEqual(got, []string{"wasi:stdio"}) {
		t.Fatalf("initial grants: %v", got)
	}

	// Replace grants; unrelated fields must survive, output must be sorted.
	if err := setPluginGrants(dir, "wago-org/wasi", []string{"wasi:random", "wasi:clock"}); err != nil {
		t.Fatal(err)
	}
	if got := pluginGrants(dir, "wago-org/wasi"); !reflect.DeepEqual(got, []string{"wasi:clock", "wasi:random"}) {
		t.Fatalf("updated grants: %v", got)
	}
	updated, err := readLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := updated.Packages["wago-org/wasi"]
	if entry.Version != "0.0.0" || !reflect.DeepEqual(decodeJSON(t, entry.Config), map[string]any{"dir": "/tmp"}) {
		t.Fatal("setPluginGrants dropped the plugin's resolved version or config")
	}

	// A plugin with no entry yet: absent grants, then created on set.
	if got := pluginGrants(dir, "wago-org/new"); len(got) != 0 {
		t.Fatalf("expected no grants for absent plugin, got %v", got)
	}
	if err := setPluginGrants(dir, "wago-org/new", []string{"net:dial"}); err != nil {
		t.Fatal(err)
	}
	if got := pluginGrants(dir, "wago-org/new"); !reflect.DeepEqual(got, []string{"net:dial"}) {
		t.Fatalf("new plugin grants: %v", got)
	}
}
