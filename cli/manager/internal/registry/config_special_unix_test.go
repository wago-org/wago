//go:build linux || darwin

package registry

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCredentialStoreRejectsFIFOWithoutBlocking(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(credentialsPath(), 0o600); err != nil {
		t.Skipf("creating FIFO: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := loadCredentials()
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("FIFO credential store = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO credential store blocked during open")
	}
}
