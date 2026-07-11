package wagocli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPluginGrantsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Seed a wago.json with a plugin entry that has other fields to preserve.
	seed := `{
      "dependencies": ["github.com/wago-org/wasi"],
      "plugins": {"wago-org/wasi": {"capabilities": ["wasi:stdio"], "after": ["x"]}}
    }`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(seed), 0o644); err != nil {
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
	m, _ := readProjectMap(dir)
	entry := m["plugins"].(map[string]any)["wago-org/wasi"].(map[string]any)
	if _, ok := entry["after"]; !ok {
		t.Fatal("setPluginGrants dropped the plugin's other fields (after)")
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
