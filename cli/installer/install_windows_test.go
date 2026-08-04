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
	items := pathSetupItems(pathTargets(t.TempDir()))
	if len(items) != 2 || items[0].label != "Yes" || items[0].value != "0" || items[1].label != "No" || items[1].value != "none" {
		t.Fatalf("Windows PATH choices = %#v", items)
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
	want := "\nSweet, Wago canary-deadbee is ready at ~\\.wago\\bin\\wago.exe\n\n" +
		"Open a new terminal.\n\n" +
		"Then install the Wago version you want:\n\n" +
		"wago version install\n"
	if got := output.String(); got != want {
		t.Fatalf("Windows warm finish:\n--- got ---\n%s--- want ---\n%s", got, want)
	}

	output.Reset()
	installer.pathAdded = false
	installer.finish("canary-deadbee", installed, true, "")
	want = "\nSweet, Wago canary-deadbee is ready at ~\\.wago\\bin\\wago.exe\n\n" +
		"Then install the Wago version you want:\n\n" +
		"wago version install\n"
	if got := output.String(); got != want {
		t.Fatalf("Windows finish without activation:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}
