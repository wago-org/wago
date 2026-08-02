package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDependenciesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if dependencies, err := Dependencies(dir); err != nil || len(dependencies) != 0 {
		t.Fatalf("Dependencies(empty) = %v, %v", dependencies, err)
	}
	added, err := AddDependency(dir, "github.com/acme/wago-timer", "^0.0.0")
	if err != nil || !added {
		t.Fatalf("AddDependency = %v, %v", added, err)
	}
	if added, _ := AddDependency(dir, "github.com/acme/wago-timer", "^0.0.0"); added {
		t.Fatal("second AddDependency reported newly added")
	}
	_, _ = AddDependency(dir, "github.com/acme/wago-redis", "^0.0.0")
	dependencies, err := Dependencies(dir)
	want := []string{"github.com/acme/wago-redis", "github.com/acme/wago-timer"}
	if err != nil || !reflect.DeepEqual(dependencies, want) {
		t.Fatalf("Dependencies = %v, %v, want %v", dependencies, err, want)
	}
	removed, module, err := RemoveDependency(dir, "github.com/acme/wago-timer")
	if err != nil || !removed || module != "github.com/acme/wago-timer" {
		t.Fatalf("RemoveDependency = %v, %q, %v", removed, module, err)
	}
}

func TestDependencyUpdatePreservesManifestFields(t *testing.T) {
	dir := t.TempDir()
	seed := `{"module":"github.com/me/thing","version":"0.0.0"}`
	if err := os.WriteFile(filepath.Join(dir, File), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddDependency(dir, "github.com/acme/wago-timer", "^0.0.0"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, File))
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["module"] != "github.com/me/thing" || manifest["version"] != "0.0.0" ||
		manifest["$schema"] != SchemaURI {
		t.Fatalf("publish fields not preserved: %v", manifest)
	}
	plugins, ok := manifest["plugins"].(map[string]any)
	if !ok || plugins["acme/wago-timer"] != "^0.0.0" {
		t.Fatalf("plugin requirement not recorded: %v", manifest["plugins"])
	}
}

func TestGrantsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	seed := `{"plugins":{"wago-org/wasi":"^0.0.0","wago-org/new":"^0.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, File), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(dir, LockDocument{Packages: map[string]LockEntry{
		"wago-org/wasi": {Version: "v0.0.0", Capabilities: json.RawMessage(`["wasi:stdio"]`), Config: json.RawMessage(`{"dir":"/tmp"}`)},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := Grants(dir, "wago-org/wasi"); !reflect.DeepEqual(got, []string{"wasi:stdio"}) {
		t.Fatalf("initial grants: %v", got)
	}
	if err := SetGrants(dir, "wago-org/wasi", []string{"wasi:random", "wasi:clock"}); err != nil {
		t.Fatal(err)
	}
	if got := Grants(dir, "wago-org/wasi"); !reflect.DeepEqual(got, []string{"wasi:clock", "wasi:random"}) {
		t.Fatalf("updated grants: %v", got)
	}
	lock, err := ReadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := lock.Packages["wago-org/wasi"]
	if entry.Version != "v0.0.0" || !jsonEqual(entry.Config, json.RawMessage(`{"dir":"/tmp"}`)) {
		t.Fatal("SetGrants dropped the resolved version or plugin config")
	}
	if err := SetGrants(dir, "wago-org/new", []string{"net:dial"}); err != nil {
		t.Fatal(err)
	}
	if got := Grants(dir, "wago-org/new"); !reflect.DeepEqual(got, []string{"net:dial"}) {
		t.Fatalf("new plugin grants: %v", got)
	}
}

func TestRequirementsRejectLegacyAndInvalidManifests(t *testing.T) {
	for _, manifest := range []string{
		`{"schema":"wago/other","plugins":[]}`,
		`{"dependencies":["github.com/wago-org/wasi"],"plugins":{"wago-org/wasi":"^0.0.0"}}`,
		`{"plugins":[{"name":"wago-org/wasi"}]}`,
		`{"plugins":{"wago-org/wasi":"newest"}}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, File), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Requirements(dir); err == nil {
			t.Fatalf("Requirements accepted legacy/invalid manifest: %s", manifest)
		}
	}
}
