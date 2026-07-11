package wagocli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeriveName(t *testing.T) {
	cases := map[string]string{
		"github.com/acme/wago-timer": "github.com/acme/wago-timer",
		"github.com/acme/wago-redis": "github.com/acme/wago-redis",
		"github.com/acme/wago_kv":    "github.com/acme/wago_kv",
		"example.com/x/y/plugin":     "example.com/x/y/plugin",
		"bare":                       "bare",
	}
	for in, want := range cases {
		if got := deriveName(in); got != want {
			t.Errorf("deriveName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectDepsRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Empty project: no deps, no file.
	if deps, err := projectDeps(dir); err != nil || len(deps) != 0 {
		t.Fatalf("projectDeps(empty) = %v, %v", deps, err)
	}

	// Add creates the file and records the module.
	newly, err := addProjectDep(dir, "github.com/acme/wago-timer")
	if err != nil || !newly {
		t.Fatalf("addProjectDep = %v, %v (want true, nil)", newly, err)
	}
	// Adding again is idempotent.
	if newly, _ := addProjectDep(dir, "github.com/acme/wago-timer"); newly {
		t.Fatal("second addProjectDep reported newly-added")
	}
	addProjectDep(dir, "github.com/acme/wago-redis")

	deps, err := projectDeps(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"github.com/acme/wago-redis", "github.com/acme/wago-timer"} // sorted
	if !reflect.DeepEqual(deps, want) {
		t.Fatalf("projectDeps = %v, want %v", deps, want)
	}

	// Remove by canonical module path.
	removed, module, err := removeProjectDep(dir, "github.com/acme/wago-timer")
	if err != nil || !removed || module != "github.com/acme/wago-timer" {
		t.Fatalf("removeProjectDep = %v, %q, %v", removed, module, err)
	}
	if deps, _ := projectDeps(dir); !reflect.DeepEqual(deps, []string{"github.com/acme/wago-redis"}) {
		t.Fatalf("after remove: %v", deps)
	}
}

// Adding a dependency preserves unrelated wago.json fields (publish metadata).
func TestAddProjectDepPreservesFields(t *testing.T) {
	dir := t.TempDir()
	seed := `{"schema":"wago-plugin/v1","module":"github.com/me/thing","version":"1.2.3"}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := addProjectDep(dir, "github.com/acme/wago-timer"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, projectFile))
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["module"] != "github.com/me/thing" || m["version"] != "1.2.3" || m["schema"] != "wago-plugin/v1" {
		t.Fatalf("publish fields not preserved: %v", m)
	}
	if deps := depsFromMap(m); len(deps) != 1 || deps[0] != "github.com/acme/wago-timer" {
		t.Fatalf("dependency not added: %v", deps)
	}
	if m["$schema"] != manifestSchemaURI {
		t.Fatalf("schema URI = %v, want %s", m["$schema"], manifestSchemaURI)
	}
	plugins, ok := m["plugins"].([]any)
	if !ok || len(plugins) != 1 || plugins[0].(map[string]any)["name"] != "github.com/acme/wago-timer" {
		t.Fatalf("plugin authority scaffold not added: %v", m["plugins"])
	}
}
