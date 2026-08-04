//go:build windows

package main

import (
	"bytes"
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

func TestWindowsPathPromptMatchesWarmInstaller(t *testing.T) {
	if got, want := pathSetupQuestion(), "Add Wago to PATH?"; got != want {
		t.Fatalf("PATH prompt = %q, want %q", got, want)
	}
}

func TestWindowsSkippedPathSetupKeepsStatus(t *testing.T) {
	t.Setenv("WAGO_PATH_SETUP", "none")
	t.Setenv("WAGO_TEST_USER_PATH", `C:\Windows\System32`)
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.offerPathSetup()
	if got, want := output.String(), "\nAdd Wago to PATH? No\n"; got != want {
		t.Fatalf("skipped PATH output = %q, want %q", got, want)
	}
}

func TestWindowsWarmFinishAfterPathSetup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.pathAdded = true
	installed := filepath.Join(home, ".wago", "bin", "wago.exe")
	installer.finish("canary-deadbee", installed, true, "")
	text := output.String()
	for _, fragment := range []string{
		"Sweet, Wago canary-deadbee is ready at ~\\.wago\\bin\\wago.exe",
		"Open a new terminal.",
		"wago version install",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("warm finish missing %q:\n%s", fragment, text)
		}
	}
}
