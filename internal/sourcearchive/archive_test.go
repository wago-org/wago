package sourcearchive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "source.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("root/../../escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Extract(archive, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("traversing source archive unexpectedly succeeded")
	}
}
