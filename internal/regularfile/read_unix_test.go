//go:build !windows

package regularfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadRejectsSymlinkAndFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "link")
	if err := os.Symlink(dir, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, 10); err == nil {
		t.Fatal("accepted symlink")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(fifo, 10); err == nil {
		t.Fatal("accepted FIFO")
	}
}
