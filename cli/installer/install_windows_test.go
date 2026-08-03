//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsPathSetupUsesUserPathWithoutShellFiles(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), ".wago", "bin")
	t.Setenv("WAGO_TEST_USER_PATH", `C:\Windows\System32`)
	already, err := addPath(binDir, "", "cmd")
	if err != nil || already {
		t.Fatalf("first PATH setup = %v, %v", already, err)
	}
	if !strings.Contains(strings.ToLower(os.Getenv("PATH")), strings.ToLower(binDir)) {
		t.Fatalf("process PATH does not contain %q", binDir)
	}

	t.Setenv("WAGO_TEST_USER_PATH", binDir+`;C:\Windows\System32`)
	already, err = addPath(binDir, "", "cmd")
	if err != nil || !already {
		t.Fatalf("existing PATH setup = %v, %v", already, err)
	}
	targets := pathTargets(t.TempDir())
	if len(targets) != 1 || targets[0].label != "Command Prompt" || targets[0].description != "User PATH" {
		t.Fatalf("Windows PATH targets = %#v", targets)
	}
}
