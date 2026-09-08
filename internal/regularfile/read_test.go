package regularfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLimitsAndRegularFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	if err := os.WriteFile(path, []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := Read(path, 4); err != nil || string(data) != "1234" {
		t.Fatalf("exact limit: %q, %v", data, err)
	}
	if _, err := Read(path, 3); err == nil {
		t.Fatal("accepted oversized file")
	}
	if _, err := Read(dir, 4); err == nil {
		t.Fatal("accepted directory")
	}
}
