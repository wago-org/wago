package wago

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallerDefaultsToWagoBin(t *testing.T) {
	home := t.TempDir()
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"WAGO_DRY_RUN=1",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("installer dry run: %v\n%s", err, output)
	}
	want := "~/.wago/bin/wago"
	if !strings.Contains(string(output), want) {
		t.Fatalf("installer output does not contain default executable %q:\n%s", want, output)
	}
}

func TestInstallerAsksWhereToInstall(t *testing.T) {
	home := t.TempDir()
	tty := filepath.Join(home, "tty")
	if err := os.WriteFile(tty, []byte("~/tools/wago\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"WAGO_INSTALL_TTY="+tty,
		"WAGO_INTERNAL_INSTALL_DIR_ONLY=1",
		"WAGO_DRY_RUN=1",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install directory prompt: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "Where should Wago be installed?") ||
		!strings.Contains(text, "Directory [~/.wago/bin]:") ||
		!strings.Contains(text, "bin=~/tools/wago") {
		t.Fatalf("installer did not ask for its location:\n%s", text)
	}
}

func TestInstallerAsksBeforeReplacingExistingInstallation(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".wago", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := filepath.Join(bin, "wago")
	if err := os.WriteFile(manager, []byte("existing manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	tty := filepath.Join(home, "tty")
	if err := os.WriteFile(tty, []byte("n\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"WAGO_BIN_DIR="+bin,
		"WAGO_INSTALL_TTY="+tty,
		"WAGO_INTERNAL_REINSTALL_CHECK_ONLY=1",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("repeat install check: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "Wago is already installed at ~/.wago/bin/wago") ||
		!strings.Contains(text, "Reinstall? [Y/n]") ||
		!strings.Contains(text, "Cancelled.") {
		t.Fatalf("repeat install skipped confirmation:\n%s", text)
	}
	body, err := os.ReadFile(manager)
	if err != nil || string(body) != "existing manager" {
		t.Fatalf("cancelled reinstall changed manager: %q, %v", body, err)
	}
}

func TestInstallerOffersPersistentPathSetupWhenAlreadyOnCurrentPath(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(home, ".wago", "bin")
	fakeBin := filepath.Join(root, "bin")
	tty := filepath.Join(root, "tty")
	for _, dir := range []string{home, bin, fakeBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	zsh := filepath.Join(fakeBin, "zsh")
	if err := os.WriteFile(zsh, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"SHELL="+zsh,
		"PATH="+bin+":"+fakeBin+":/usr/bin:/bin",
		"WAGO_BIN_DIR="+bin,
		"WAGO_INSTALL_TTY="+tty,
		"WAGO_INTERNAL_PATH_SETUP_ONLY=1",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("persistent PATH setup: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "Add Wago to your PATH?") ||
		!strings.Contains(text, "Added Wago to PATH in ~/.zshrc") {
		t.Fatalf("installer trusted only the current process PATH:\n%s", text)
	}
}

func TestInstallerOffersCurrentShellPathSetupIdempotently(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(home, "custom bin")
	fakeBin := filepath.Join(root, "bin")
	tty := filepath.Join(root, "tty")
	for _, dir := range []string{home, fakeBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	zsh := filepath.Join(fakeBin, "zsh")
	if err := os.WriteFile(zsh, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func() string {
		t.Helper()
		command := exec.Command("sh", "install.sh")
		command.Env = append(os.Environ(),
			"HOME="+home,
			"SHELL="+zsh,
			"PATH="+fakeBin+":/usr/bin:/bin",
			"WAGO_BIN_DIR="+bin,
			"WAGO_INSTALL_TTY="+tty,
			"WAGO_INTERNAL_PATH_SETUP_ONLY=1",
			"NO_COLOR=1",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("path setup: %v\n%s", err, output)
		}
		return string(output)
	}

	output := run()
	if !strings.Contains(output, "zsh") ||
		!strings.Contains(output, "Added Wago to PATH in ~/.zshrc") ||
		!strings.Contains(output, "Open a new shell to use wago or run source ~/.zshrc") {
		t.Fatalf("path setup output:\n%s", output)
	}
	config := filepath.Join(home, ".zshrc")
	body, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	want := "export PATH='" + bin + "':\"$PATH\""
	if !strings.Contains(string(body), want) {
		t.Fatalf(".zshrc = %q, want %q", body, want)
	}

	output = run()
	if !strings.Contains(output, "already configured") {
		t.Fatalf("repeat path setup output:\n%s", output)
	}
	body, err = os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "# Wago PATH:") != 1 {
		t.Fatalf("path setup is not idempotent:\n%s", body)
	}
}

func TestInstallerFallsBackFromGitToSourceArchive(t *testing.T) {
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "git"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "wago.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("wago-main/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("module github.com/wago-org/wago\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	curl := `#!/bin/sh
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		cp "$WAGO_TEST_ARCHIVE" "$2"
		exit $?
	fi
	shift
done
exit 1
`
	if err := os.WriteFile(filepath.Join(fakeBin, "curl"), []byte(curl), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+":/usr/bin:/bin",
		"WAGO_INTERNAL_FETCH_ONLY=1",
		"WAGO_ARCHIVE_URL=https://example.invalid/wago.zip",
		"WAGO_TEST_ARCHIVE="+archive,
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("archive fallback: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "git fetch failed; trying source archive") ||
		!strings.Contains(text, "source=archive") {
		t.Fatalf("archive fallback output:\n%s", text)
	}
}

func TestInstallerVerificationCannotHang(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	wago := filepath.Join(bin, "wago")
	if err := os.WriteFile(wago, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	command := exec.CommandContext(ctx, "sh", "install.sh")
	command.Env = append(os.Environ(),
		"WAGO_BIN_DIR="+bin,
		"WAGO_INTERNAL_VERIFY_ONLY=1",
		"WAGO_VERIFY_TIMEOUT=1",
		"NO_COLOR=1",
	)
	err := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("installer verification hung for %s", time.Since(start).Round(time.Millisecond))
	}
	if err == nil {
		t.Fatal("hanging verification unexpectedly succeeded")
	}
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Fatalf("installer verification took %s, want a bounded failure", elapsed.Round(time.Millisecond))
	}
}
