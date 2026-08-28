//go:build windows

package artifactcache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCacheRootRejectsReparsePoint(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "cache-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	root, err := openCacheRoot(link)
	if err == nil {
		root.close()
		t.Fatal("opened reparse-point cache root")
	}
}
