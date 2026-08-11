//go:build windows

package atomicfile

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsReplaceExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.exe")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(path, Options{Mode: 0o755, Sync: true}, func(writer io.Writer) error {
		_, err := writer.Write([]byte("new"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("replacement = %q, %v", data, err)
	}
}
