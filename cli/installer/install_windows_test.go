//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsConsoleInputUsesConsoleDeviceWhenStdinIsRedirected(t *testing.T) {
	redirectedRead, redirectedWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = redirectedRead.Close()
		_ = redirectedWrite.Close()
	})
	oldStdin := os.Stdin
	os.Stdin = redirectedRead
	t.Cleanup(func() { os.Stdin = oldStdin })

	consoleInput, err := os.CreateTemp(t.TempDir(), "console-input")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consoleInput.Close() })
	oldOpen := openConsoleDevice
	openConsoleDevice = func() (*os.File, error) { return consoleInput, nil }
	t.Cleanup(func() { openConsoleDevice = oldOpen })

	input := consoleInputFile()
	if input == os.Stdin || input.Fd() != consoleInput.Fd() {
		t.Fatalf("console input = %v, want dedicated console device %v", input, consoleInput)
	}
}

func TestWindowsConsoleInputFallsBackWithoutConsole(t *testing.T) {
	oldOpen := openConsoleDevice
	openConsoleDevice = func() (*os.File, error) { return nil, os.ErrNotExist }
	t.Cleanup(func() { openConsoleDevice = oldOpen })

	if input := consoleInputFile(); input != os.Stdin {
		t.Fatalf("console input = %v, want stdin fallback %v", input, os.Stdin)
	}
}

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

func TestWindowsInstallPathsDoNotAssumeCDrive(t *testing.T) {
	home := `D:\Users\wago`
	binDir := filepath.Join(home, ".wago", "bin")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WAGO_BIN_DIR", binDir)
	t.Setenv("WAGO_TEST_USER_PATH", `D:\Windows\System32`)
	t.Setenv("NO_COLOR", "1")

	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.installLocation()
	if got, want := output.String(), "Install location: ~\\.wago\\bin\n"; got != want {
		t.Fatalf("install location = %q, want %q", got, want)
	}
	if already, err := addPath(binDir, "", "cmd"); err != nil || already {
		t.Fatalf("D: PATH setup = %v, %v", already, err)
	}
	if !pathContains(binDir) {
		t.Fatalf("process PATH does not contain D: install directory %q", binDir)
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
		"Now, install the Wago version you want:\n\n" +
		"wago version install\n"
	if got := output.String(); got != want {
		t.Fatalf("Windows finish without activation:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}
