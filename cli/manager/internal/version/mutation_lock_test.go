package version

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/wagopaths"
)

func TestVersionMutationSerializesRemovalAndSelection(t *testing.T) {
	d := activeStateTestDirs(t)
	path := d.RuntimeBinary("canary", "standard", "normal")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("runtime"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := useInstalledVersion(context.Background(), d, "canary", "", ""); err != nil {
		t.Fatal(err)
	}
	lock, err := versionMutationLock(context.Background(), d, "canary")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	lockPath := filepath.Join(d.Config, "version-locks", "canary.lock")
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	go func() { removed <- removeInstalledVersion(d, "canary") }()
	select {
	case err := <-removed:
		t.Fatalf("removal ignored held version lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("locked payload changed: %v", err)
	}
	// A different version does not contend with this operation.
	other, err := versionMutationLock(context.Background(), d, "nightly")
	if err != nil {
		t.Fatal(err)
	}
	_ = other.Close()
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-removed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("removal did not resume")
	}
	if _, _, err := useInstalledVersion(context.Background(), d, "canary", wagopaths.ProfileStandard, wagopaths.BuildNormal); err == nil {
		t.Fatal("selected removed runtime")
	}
	if state, err := readActiveInstallation(d); err != nil || state.Version != "" {
		t.Fatalf("removed active state: %+v, %v", state, err)
	}
	after, err := os.Stat(lockPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("removal retired the stable coordinator: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("reinstalled"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := useInstalledVersion(context.Background(), d, "canary", "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestVersionMutationWaitHonorsCancellation(t *testing.T) {
	d := activeStateTestDirs(t)
	lock, err := versionMutationLock(context.Background(), d, "canary")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if next, err := versionMutationLock(ctx, d, "canary"); !errors.Is(err, context.DeadlineExceeded) {
		if next != nil {
			next.Close()
		}
		t.Fatalf("contended version lock: %v", err)
	}
}
