//go:build windows

package replace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStageRemovalMovesExecutableOutsideContainingTarget(t *testing.T) {
	target := t.TempDir()
	executable := filepath.Join(target, "bin", "wago.exe")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("wago"), 0o755); err != nil {
		t.Fatal(err)
	}

	staged, err := StageRemoval(executable, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(staged) })
	if samePath(staged, executable) {
		t.Fatalf("StageRemoval() = %q, executable was not moved", staged)
	}
	if containsPath(target, staged) {
		t.Fatalf("StageRemoval() = %q, still inside cleanup target %q", staged, target)
	}
	if _, err := os.Stat(executable); !os.IsNotExist(err) {
		t.Fatalf("original executable still exists: %v", err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staged executable: %v", err)
	}
}
