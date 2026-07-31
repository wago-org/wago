package project

import (
	"os"
	"path/filepath"
	"testing"
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
	manifest["name"] = "example"
	if err := Write(dir, manifest); err != nil {
		t.Fatal(err)
	}
	created, err = Initialize(dir)
	if err != nil || created {
		t.Fatalf("Initialize repeat = %v, %v", created, err)
	}
	manifest, _ = Read(dir)
	if manifest["name"] != "example" {
		t.Fatalf("repeat initialization discarded fields: %#v", manifest)
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
