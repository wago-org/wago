package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
)

func TestInitializeCreatesManifestAndPreservesFields(t *testing.T) {
	dir := t.TempDir()
	created, err := Initialize(dir)
	if err != nil || !created {
		t.Fatalf("Initialize first = %v, %v", created, err)
	}
	manifest, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest["$schema"] != SchemaURI {
		t.Fatalf("initialized metadata = %#v", manifest)
	}
	if _, exists := manifest["schema"]; exists {
		t.Fatalf("initialized manifest contains removed schema version: %#v", manifest)
	}
	manifest["package"] = validTestPackage()
	if err := Write(dir, manifest); err != nil {
		t.Fatal(err)
	}
	created, err = Initialize(dir)
	if err != nil || created {
		t.Fatalf("Initialize repeat = %v, %v", created, err)
	}
	manifest, _ = Read(dir)
	if manifest["package"].(map[string]any)["name"] != "Example" {
		t.Fatalf("repeat initialization discarded fields: %#v", manifest)
	}
}

func TestLockedModeRejectsManifestAndLockfileWrites(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	t.Setenv(automation.EnvLocked, "1")
	dir := t.TempDir()
	if err := Write(dir, map[string]any{"name": "demo"}); err == nil || !strings.Contains(err.Error(), File) {
		t.Fatalf("manifest write error = %v", err)
	}
	if err := WriteLock(dir, LockDocument{}); err == nil || !strings.Contains(err.Error(), LockFile) {
		t.Fatalf("lockfile write error = %v", err)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Fatalf("locked manifest write changed the filesystem: %v", err)
	}
	if _, err := os.Stat(LockPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("locked lockfile write changed the filesystem: %v", err)
	}
}

func TestInitializeWithMergesWizardFields(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitializeWith(dir, map[string]any{"package": validTestPackage()}); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeWith(dir, map[string]any{"settings": map[string]any{"runtime": map[string]any{"parallel": "auto"}}}); err != nil {
		t.Fatal(err)
	}
	manifest, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	pkg := manifest["package"].(map[string]any)
	if pkg["name"] != "Example" || manifest["settings"] == nil {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestManifestValidationRejectsUnknownNestedAndMalformedFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"root", func(manifest map[string]any) { manifest["name"] = "legacy" }},
		{"package", func(manifest map[string]any) { manifest["package"].(map[string]any)["future"] = true }},
		{"author", func(manifest map[string]any) {
			manifest["package"].(map[string]any)["authors"].([]any)[0].(map[string]any)["handle"] = "acme"
		}},
		{"setting", func(manifest map[string]any) {
			manifest["settings"] = map[string]any{"runtime": map[string]any{"workers": 2}}
		}},
		{"subpackage outside module", func(manifest map[string]any) {
			manifest["package"].(map[string]any)["subpackages"] = []any{map[string]any{
				"module": "example.com/other/plugin", "name": "Other", "description": "Other provider",
			}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := map[string]any{"$schema": SchemaURI, "package": validTestPackage(), "plugins": map[string]any{}}
			test.mutate(manifest)
			if _, err := EncodeManifest(manifest); err == nil {
				t.Fatalf("EncodeManifest accepted %#v", manifest)
			}
		})
	}
}

func TestManifestValidationAcceptsCataloguedOptimizationFamilies(t *testing.T) {
	optimizations := map[string]any{}
	for _, name := range []string{
		"simd-superopt", "swar-idioms", "interval-region-pins", "fcmp-fuse", "magic-div",
		"shared-trap-body", "shared-adapters", "zero-branch", "mul-add-fuse",
		"entry-init-elision", "v128-direct-results", "dead-gc-new", "gc-ref-facts",
		"gc-native-alloc",
	} {
		optimizations[name] = false
	}
	manifest := map[string]any{
		"$schema":  SchemaURI,
		"settings": map[string]any{"optimizations": optimizations},
	}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatal(err)
	}
}

func validTestPackage() map[string]any {
	return map[string]any{
		"module": "github.com/acme/example", "version": "1.2.3", "name": "Example",
		"description": "Example plugin.", "license": "MIT", "repository": "https://github.com/acme/example",
		"authors": []any{map[string]any{"name": "A. Maintainer"}},
	}
}

func TestEnsureGitignoreIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	EnsureGitignore(".wago/")
	EnsureGitignore(".wago/")
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil || string(data) != ".wago/\n" {
		t.Fatalf(".gitignore = %q, %v", data, err)
	}
}
