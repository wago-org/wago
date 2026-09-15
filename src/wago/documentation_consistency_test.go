package wago

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepositoryStatusDocuments keeps mechanically checkable architecture facts
// from drifting away from the implementation. Design rationale remains prose;
// only stable markers are enforced here.
func TestRepositoryStatusDocuments(t *testing.T) {
	root := repositoryRoot(t)

	architecture := readRepositoryDocument(t, root, "ARCHITECTURE.md")
	for _, marker := range []string{
		"<!-- architecture:targets linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 -->",
		fmt.Sprintf("<!-- artifact:codec-version %d -->", wagoVersion),
	} {
		if !strings.Contains(architecture, marker) {
			t.Errorf("ARCHITECTURE.md missing implementation marker %q", marker)
		}
	}

	versioning := readRepositoryDocument(t, root, "VERSIONING.md")
	for _, marker := range []string{"compiled `.wago` executable codec uses **version 2**", "older artifact cannot bypass a stricter runtime configuration"} {
		if !strings.Contains(versioning, marker) {
			t.Errorf("VERSIONING.md missing pre-release format policy marker %q", marker)
		}
	}

	roadmap := readRepositoryDocument(t, root, "ROADMAP.md")
	if marker := "<!-- roadmap:P1 status=done -->"; !strings.Contains(roadmap, marker) {
		t.Errorf("ROADMAP.md missing landed CodegenStats marker %q", marker)
	}

}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat go.mod: %v", err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repository root not found from %s", dir)
		}
		dir = parent
	}
}

func readRepositoryDocument(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
