package wasmtimecorpus

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCorpusTreeRejectsOrphansAndNoncontiguousDirectArtifacts(t *testing.T) {
	makeTree := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		dir := filepath.Join(root, "direct")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "source.wast"), []byte("source"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "module.0.wasm"), []byte("wasm"), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	fixtures := []Fixture{{Path: "direct.wast", Coverage: "runtime-regression", Mode: ModeDirectGo}}
	t.Run("valid", func(t *testing.T) {
		if err := ValidateCorpusTree(makeTree(t), fixtures); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("orphan file", func(t *testing.T) {
		root := makeTree(t)
		if err := os.WriteFile(filepath.Join(root, "direct", "notes.txt"), []byte("orphan"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ValidateCorpusTree(root, fixtures); err == nil {
			t.Fatal("orphan file was accepted")
		}
	})
	t.Run("noncontiguous", func(t *testing.T) {
		root := makeTree(t)
		if err := os.WriteFile(filepath.Join(root, "direct", "module.2.wasm"), []byte("wasm"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ValidateCorpusTree(root, fixtures); err == nil {
			t.Fatal("noncontiguous direct artifact was accepted")
		}
	})
	t.Run("orphan directory", func(t *testing.T) {
		root := makeTree(t)
		if err := os.Mkdir(filepath.Join(root, "orphan"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := ValidateCorpusTree(root, fixtures); err == nil {
			t.Fatal("orphan directory was accepted")
		}
	})
}
