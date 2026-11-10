//go:build windows

package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/wagopaths"
	"golang.org/x/sys/windows"
)

func TestCleanReportsFilesRemovedBeforeWindowsSharingFailure(t *testing.T) {
	workingDir := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	versions := filepath.Join(t.TempDir(), "versions")
	plugins := filepath.Join(versions, "v1", "standard", "normal", "plugins")
	if err := os.MkdirAll(plugins, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.wago", "second.wago"} {
		if err := os.WriteFile(filepath.Join(plugins, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// ReadDir on an opened directory preserves enumeration order. Block the
	// second entry so cleanup must report the first successful deletion.
	dir, err := os.Open(plugins)
	if err != nil {
		t.Fatal(err)
	}
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read plugin entries: %v; close: %v", readErr, closeErr)
	}
	if len(entries) != 2 {
		t.Fatalf("plugin entries = %d, want 2", len(entries))
	}
	first := filepath.Join(plugins, entries[0].Name())
	blocked := filepath.Join(plugins, entries[1].Name())
	name, err := windows.UTF16PtrFromString(blocked)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(blocker)

	result, err := Clean(wagopaths.Dirs{Versions: versions}, Selection{Builds: true})
	if err == nil {
		t.Fatal("cleanup unexpectedly succeeded despite a sharing violation")
	}
	if result.Bytes != int64(len(entries[0].Name())) || result.Removed != 0 {
		t.Fatalf("partial cleanup result = %+v, want first file's %d bytes and no completed plugins leaf", result, len(entries[0].Name()))
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("first file was not deleted: %v", err)
	}
	if _, err := os.Stat(blocked); err != nil {
		t.Fatalf("blocked file was unexpectedly deleted: %v", err)
	}
}
