package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one"), make([]byte, 1025), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := walkedSize(dir); got != 1025 {
		t.Fatalf("walkedSize = %d", got)
	}
	if got := gitTrackedSize(dir); got != -1 {
		t.Fatalf("gitTrackedSize non-repo = %d", got)
	}
	if got := UnpackedKB(dir); got != 2 {
		t.Fatalf("UnpackedKB = %d", got)
	}
	if got := UnpackedKB(filepath.Join(dir, "missing")); got != 0 {
		t.Fatalf("missing UnpackedKB = %d", got)
	}
	if GitOutput("definitely-not-a-git-command") != "" {
		t.Fatal("failed GitOutput was non-empty")
	}
}

func TestInlineManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "grandchild.json"), []byte(`{"name":"grandchild"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "child.json")
	if err := os.WriteFile(child, []byte(`{"name":"child","subpackages":["./grandchild.json"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	inline, err := InlineManifest([]byte(`{"name":"root","subpackages":["./child.json",{"name":"inline"}]}`), dir)
	if err != nil || !strings.Contains(string(inline), `"name":"grandchild"`) ||
		strings.Contains(string(inline), "./child.json") {
		t.Fatalf("InlineManifest = %s, %v", inline, err)
	}
	if _, err := InlineManifest([]byte("not json"), dir); err == nil {
		t.Fatal("invalid manifest accepted")
	}
	if _, err := InlineManifest([]byte(`{"subpackages":["missing.json"]}`), dir); err == nil {
		t.Fatal("missing subpackage accepted")
	}
	if err := os.WriteFile(child, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InlineManifest([]byte(`{"subpackages":["child.json"]}`), dir); err == nil {
		t.Fatal("invalid child accepted")
	}
}
