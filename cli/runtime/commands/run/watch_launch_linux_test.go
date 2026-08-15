//go:build linux && !wago_lean

package run

import (
	"errors"
	"strings"
	"testing"
)

func TestWatchedChildLaunchUsesManagerAsSupervisor(t *testing.T) {
	t.Setenv("WAGO_MANAGER_EXECUTABLE", "/opt/wago")
	var probed string
	probe := func(manager string) error {
		probed = manager
		return nil
	}
	executable, environment, err := watchedChildLaunchWithProbe(probe)
	if err != nil {
		t.Fatal(err)
	}
	if executable != "/opt/wago" {
		t.Fatalf("watch supervisor = %q, want /opt/wago", executable)
	}
	if probed != executable {
		t.Fatalf("probed manager = %q, want %q", probed, executable)
	}
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "WAGO_WATCH_SUPERVISOR=guardian") ||
		!strings.Contains(joined, "WAGO_WATCH_GUEST_EXECUTABLE=") {
		t.Fatalf("watch supervisor environment is missing its handoff: %q", joined)
	}
}

func TestWatchedChildLaunchRejectsOldManager(t *testing.T) {
	t.Setenv("WAGO_MANAGER_EXECUTABLE", "/opt/wago")
	probe := func(string) error { return errors.New("unsupported") }
	if _, _, err := watchedChildLaunchWithProbe(probe); err == nil || !strings.Contains(err.Error(), "compatible wago manager") {
		t.Fatalf("old manager error = %v", err)
	}
}

func TestWatchedChildLaunchRequiresManager(t *testing.T) {
	t.Setenv("WAGO_MANAGER_EXECUTABLE", "")
	if _, _, err := watchedChildLaunch(); err == nil {
		t.Fatal("direct Linux runtime watch unexpectedly succeeded")
	}
}
