package wago

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRepositoryStatusDocuments keeps mechanically checkable architecture facts
// from drifting away from the implementation. Design rationale remains prose;
// only stable markers and the placement of dated snapshots are enforced here.
func TestRepositoryStatusDocuments(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))

	architecture := readRepositoryDocument(t, root, "ARCHITECTURE.md")
	for _, marker := range []string{
		"<!-- architecture:targets linux/amd64 linux/arm64 darwin/arm64 -->",
		fmt.Sprintf("<!-- artifact:codec-version %d -->", wagoVersion),
	} {
		if !strings.Contains(architecture, marker) {
			t.Errorf("ARCHITECTURE.md missing implementation marker %q", marker)
		}
	}

	roadmap := readRepositoryDocument(t, root, "ROADMAP.md")
	if marker := "<!-- roadmap:P1 status=done -->"; !strings.Contains(roadmap, marker) {
		t.Errorf("ROADMAP.md missing landed CodegenStats marker %q", marker)
	}

	for _, staleRoot := range []string{"HANDOFF.md", "status.md"} {
		if _, err := os.Stat(filepath.Join(root, staleRoot)); err == nil {
			t.Errorf("dated branch snapshot %s must live under docs/archive", staleRoot)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", staleRoot, err)
		}
	}
	for _, archived := range []string{
		"docs/archive/handoffs/2026-07-09-jairus-arm64.md",
		"docs/archive/status/2026-07-10-arm64-runtime-perf.md",
	} {
		readRepositoryDocument(t, root, archived)
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
