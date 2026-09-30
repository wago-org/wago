//go:build windows

package atomicfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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

func TestWindowsReplaceRetriesSharingViolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.exe")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	pathPointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := syscall.CreateFile(
		pathPointer,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- ReplaceFile(path, Options{Mode: 0o755}, func(writer io.Writer) error {
			_, err := writer.Write([]byte("new"))
			return err
		})
	}()
	// Windows readers do not necessarily share delete access. Keep the old file
	// open long enough to force at least one replacement attempt to contend.
	time.Sleep(50 * time.Millisecond)
	if err := syscall.CloseHandle(reader); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(windowsReplaceRetryTimeout + time.Second):
		t.Fatal("replacement did not complete after reader closed")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("replacement = %q, %v", data, err)
	}
}

func TestWindowsReplacePreservesOpenReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials-\u00e9-\U0001f512.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	pathPointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(pathPointer, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	reader := os.NewFile(uintptr(handle), path)
	defer reader.Close()
	for index := 0; index < 20; index++ {
		contents := fmt.Sprintf("new-%d", index)
		if err := ReplaceFile(path, Options{Mode: 0o600, Sync: true}, writeString(contents)); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != contents {
			t.Fatalf("new reader = %q, %v; want %q", data, err, contents)
		}
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "old" {
		t.Fatalf("existing reader = %q, %v; want old", data, err)
	}
	assertNoTemps(t, filepath.Dir(path))
}
