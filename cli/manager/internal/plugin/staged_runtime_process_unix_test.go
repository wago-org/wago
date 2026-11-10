//go:build linux || darwin

package plugin

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBoundStagedRuntimeStartFailureDoesNotPanic(t *testing.T) {
	command := exec.Command(filepath.Join(t.TempDir(), "missing-helper"))
	if err := runBoundStagedRuntime(command); err == nil {
		t.Fatal("missing validation helper unexpectedly started")
	}
}
