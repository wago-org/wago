//go:build linux && !wago_lean

package run

import (
	"strings"
	"testing"
)

func TestWatchedChildLaunchUsesManagerAsSupervisor(t *testing.T) {
	t.Setenv("WAGO_MANAGER_EXECUTABLE", "/opt/wago")
	executable, environment, err := watchedChildLaunch()
	if err != nil {
		t.Fatal(err)
	}
	if executable != "/opt/wago" {
		t.Fatalf("watch supervisor = %q, want /opt/wago", executable)
	}
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "WAGO_WATCH_SUPERVISOR=guardian") ||
		!strings.Contains(joined, "WAGO_WATCH_GUEST_EXECUTABLE=") {
		t.Fatalf("watch supervisor environment is missing its handoff: %q", joined)
	}
}

func TestWatchedChildLaunchRequiresManager(t *testing.T) {
	t.Setenv("WAGO_MANAGER_EXECUTABLE", "")
	if _, _, err := watchedChildLaunch(); err == nil {
		t.Fatal("direct Linux runtime watch unexpectedly succeeded")
	}
}
