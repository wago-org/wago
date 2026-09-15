package managedrelease

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/filelock"
)

func TestPrepareWaitsForPublicationLock(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), executableName())
	lock, err := filelock.Acquire(context.Background(), PublicationLockPath(launcher))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	writing := make(chan struct{})
	done := make(chan error, 1)
	stop := errors.New("stop before writing fixture")
	go func() {
		_, err := Prepare(launcher, "test", func(string, string) error { close(writing); return stop }, nil)
		done <- err
	}()
	select {
	case <-writing:
		t.Error("preparation started while publication lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, stop) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preparation did not resume")
	}
}
