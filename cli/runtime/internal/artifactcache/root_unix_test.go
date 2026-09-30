//go:build linux || darwin

package artifactcache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRootRemoveDoesNotFollowReplacedParent(t *testing.T) {
	rootPath := t.TempDir()
	parent := filepath.Join(rootPath, "aa")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := openCacheRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.close()

	outside := t.TempDir()
	outsideArtifact := filepath.Join(outside, "outside.wago")
	if err := os.WriteFile(outsideArtifact, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, parent+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}

	if err := root.remove(filepath.Join("aa", "outside.wago")); err == nil {
		t.Fatal("cache removal followed replaced parent")
	}
	contents, err := os.ReadFile(outsideArtifact)
	if err != nil {
		t.Fatalf("outside artifact removed: %v", err)
	}
	if string(contents) != "keep" {
		t.Fatalf("outside artifact contents = %q", contents)
	}
}
