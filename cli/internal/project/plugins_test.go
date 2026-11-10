package project

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRequirementsUseCanonicalFullIDsAndFullSemverRanges(t *testing.T) {
	dir := t.TempDir()
	for id, constraint := range map[string]string{
		"github.com/acme/wago-timer": ">=1.2.0 <2.0.0 || 3.x",
		"example.com/plugins/redis":  "1.2.3 - 2.4.0",
	} {
		if added, err := AddDependency(dir, id, constraint); err != nil || !added {
			t.Fatalf("AddDependency(%q) = %v, %v", id, added, err)
		}
	}
	dependencies, err := Dependencies(dir)
	want := []string{"example.com/plugins/redis", "github.com/acme/wago-timer"}
	if err != nil || !reflect.DeepEqual(dependencies, want) {
		t.Fatalf("Dependencies = %v, %v, want %v", dependencies, err, want)
	}
	removed, id, err := RemoveDependency(dir, "github.com/acme/wago-timer")
	if err != nil || !removed || id != "github.com/acme/wago-timer" {
		t.Fatalf("RemoveDependency = %v, %q, %v", removed, id, err)
	}
}

func TestDependencyUpdatePreservesPackageField(t *testing.T) {
	dir := t.TempDir()
	seed := `{"$schema":"https://wago.sh/v1/schema.json","package":{"module":"github.com/me/thing","version":"1.0.0","name":"Thing","description":"A thing.","license":"MIT","repository":"https://github.com/me/thing","authors":[{"name":"A. Maintainer"}]},"plugins":{}}`
	if err := os.WriteFile(filepath.Join(dir, File), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddDependency(dir, "github.com/acme/wago-timer", "^1.0.0"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, File))
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["$schema"] != SchemaURI || manifest["package"] == nil {
		t.Fatalf("package field not preserved: %v", manifest)
	}
	plugins := manifest["plugins"].(map[string]any)
	if plugins["github.com/acme/wago-timer"] != "^1.0.0" {
		t.Fatalf("full plugin requirement not recorded: %v", plugins)
	}
}

func TestRemoveDependencyPublishesPrunedCoherentMetadata(t *testing.T) {
	dir := t.TempDir()
	removedID := "github.com/acme/pool"
	retainedID := "github.com/acme/logger"
	transitiveID := "github.com/acme/workers"
	manifest := map[string]any{
		"$schema": SchemaURI,
		"plugins": map[string]any{removedID: "^1.0.0", retainedID: "^1.0.0"},
	}
	lock := NewLockDocument()
	lock.Plugins[removedID] = testLockEntry(true, removedID, map[string]string{transitiveID: "^1.0.0"})
	lock.Plugins[retainedID] = testLockEntry(true, retainedID, map[string]string{})
	lock.Plugins[transitiveID] = testLockEntry(false, transitiveID, map[string]string{})
	if err := WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		return mutation.PublishMetadata(manifest, lock)
	}); err != nil {
		t.Fatal(err)
	}

	removed, id, err := RemoveDependency(dir, removedID)
	if err != nil || !removed || id != removedID {
		t.Fatalf("RemoveDependency = %v, %q, %v", removed, id, err)
	}
	gotManifest, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotLock, err := ReadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := RequirementsFromManifest(gotManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLockedResolution(requirements, gotLock); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotLock.Plugins[removedID]; ok {
		t.Fatalf("removed direct plugin remains in lock: %#v", gotLock.Plugins)
	}
	if _, ok := gotLock.Plugins[transitiveID]; ok {
		t.Fatalf("unreachable transitive plugin remains in lock: %#v", gotLock.Plugins)
	}
	if _, ok := gotLock.Plugins[retainedID]; !ok {
		t.Fatalf("unrelated direct plugin was pruned: %#v", gotLock.Plugins)
	}
}

func TestRequirementsRejectV0AliasesAndInvalidManifests(t *testing.T) {
	for _, manifest := range []string{
		`{"$schema":"https://wago.sh/v0/schema.json","plugins":{"wago-org/wasi":"^0.0.0"}}`,
		`{"schema":"wago/other","plugins":[]}`,
		`{"dependencies":["github.com/wago-org/wasi"],"plugins":{}}`,
		`{"plugins":[{"name":"github.com/wago-org/wasi"}]}`,
		`{"plugins":{"wago-org/wasi":"^1.0.0"}}`,
		`{"plugins":{"github.com/wago-org/wasi":"newest"}}`,
		`{"plugins":{"github.com/wago-org/wasi":""}}`,
		`{"plugins":{"github.com/wago-org/wasi":"   "}}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, File), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Requirements(dir); err == nil {
			t.Fatalf("Requirements accepted v0/invalid manifest: %s", manifest)
		}
	}
}

func TestValidatePluginIDRejectsRelativeAndNonCanonical(t *testing.T) {
	for _, id := range []string{"wago-org/wasi", "github.com//wasi", "github.com/acme/plugin@v1", "github.com/acme/plug+in", " github.com/acme/plugin", "github.com/acme/../plugin", "github.com/acme/./plugin", "github.com/acme/plugin "} {
		if err := ValidatePluginID(id); err == nil {
			t.Errorf("ValidatePluginID(%q) accepted", id)
		}
	}
	if err := ValidatePluginID("github.com/acme/plugin/providers/fast"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePluginID("gopkg.in/yaml.v3"); err != nil {
		t.Fatal(err)
	}
}

func TestExpandGitHubPluginID(t *testing.T) {
	for input, want := range map[string]string{
		"wago-org/wasi":                    "github.com/wago-org/wasi",
		"wago-org/wasi/providers/preview1": "github.com/wago-org/wasi/providers/preview1",
		" github.com/wago-org/wasi ":       "github.com/wago-org/wasi",
		"github.com/wago-org":              "github.com/wago-org",
		"gitlab.com/wago-org/wasi":         "gitlab.com/wago-org/wasi",
		"wago-org":                         "wago-org",
		"wago org/wasi":                    "wago org/wasi",
	} {
		if got := ExpandGitHubPluginID(input); got != want {
			t.Errorf("ExpandGitHubPluginID(%q) = %q; want %q", input, got, want)
		}
	}
}

func TestValidateConstraintUsesFullRangeGrammar(t *testing.T) {
	for _, valid := range []string{"*", ">=1.0.0 <2.0.0", "1.2.x || >=3.0.0", "1.2.3 - 2.0.0", "^1.2.3"} {
		if err := ValidateConstraint(valid); err != nil {
			t.Errorf("valid range %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "newest", ">=", "1..2"} {
		if err := ValidateConstraint(invalid); err == nil {
			t.Errorf("invalid range %q accepted", invalid)
		}
	}
	if err := ValidateConstraint(" latest "); err == nil {
		t.Fatalf("unexpected latest error: %v", err)
	}
}
