//go:build !windows

package wago

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallerRadioPreservesTerminalNewlineProcessing(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("BSD script invocation exercises the macOS terminal behavior")
	}
	home := t.TempDir()
	bin := filepath.Join(home, ".wago", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "wago"), []byte("manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(home, "terminal.log")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "script", "-q", transcript, "./install.sh")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"WAGO_BIN_DIR="+bin,
		"WAGO_INTERNAL_REINSTALL_CHECK_ONLY=1",
		"NO_COLOR=1",
		"TERM=xterm",
	)
	command.Stdin = strings.NewReader("\x1b")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installer terminal session: %v\n%s", err, output)
	}
	data, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(data, []byte("How should it be reinstalled?"))
	end := bytes.LastIndex(data, []byte("Cancelled."))
	if start < 0 || end <= start {
		t.Fatalf("terminal transcript missing radio frame:\n%s", data)
	}
	for i, value := range data[start:end] {
		if value == '\n' && (i == 0 || data[start+i-1] != '\r') {
			t.Fatalf("radio frame emitted LF without CR at byte %d; terminal output will staircase", i)
		}
	}
}

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
	for _, unwanted := range []string{"profile", "runtime"} {
		if strings.Contains(string(output), unwanted) {
			t.Fatalf("manager-only installer output unexpectedly contains %q:\n%s", unwanted, output)
		}
	}
}

func TestInstallerAsksWhereToInstall(t *testing.T) {
	home := t.TempDir()
	tty := filepath.Join(home, "tty")
	if err := os.WriteFile(tty, []byte("2\n~/tools/wago\n"), 0o600); err != nil {
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
	if !strings.HasPrefix(text, "Welcome to Wago! Let's get you set up.\n\nWhere should Wago be installed?\n") ||
		!strings.Contains(text, "› ◉ ~/.wago/bin") ||
		!strings.Contains(text, "○ Custom") ||
		!strings.Contains(text, "› ◉ Type a directory") ||
		!strings.Contains(text, "tab/→ complete") ||
		!strings.Contains(text, "Installing to: ~/tools/wago") ||
		!strings.Contains(text, "bin=~/tools/wago") {
		t.Fatalf("installer did not ask for its location:\n%s", text)
	}
}

func TestInstallerCustomPathPreviewFiltersDirectories(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"workspace", "worktree", "other", "workfile"} {
		path := filepath.Join(home, name)
		if name == "workfile" {
			if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"WAGO_INTERNAL_PATH_PREVIEW_ONLY=~/wor",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("custom path preview: %v\n%s", err, output)
	}
	text := string(output)
	for _, want := range []string{"~/workspace/", "~/worktree/"} {
		if !strings.Contains(text, want) {
			t.Fatalf("custom path preview missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"~/other/", "~/workfile"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("custom path preview included %q:\n%s", unwanted, text)
		}
	}
}

func TestInstallerCustomPathLeftArrowReturnsToParent(t *testing.T) {
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"WAGO_INTERNAL_PATH_PARENT_ONLY=~/.agents/",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("custom path parent: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "~/" {
		t.Fatalf("custom path parent = %q, want %q", got, "~/")
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
	if err := os.WriteFile(tty, []byte("esc\n"), 0o600); err != nil {
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
		!strings.Contains(text, "How should it be reinstalled?") ||
		!strings.Contains(text, "Full     Reset everything, including plugins and settings") ||
		!strings.Contains(text, "Partial  Reset Wago but keep global plugins for reinstall") ||
		!strings.Contains(text, "Minimal  Replace binaries and keep existing state") ||
		!strings.Contains(text, "○ Full") ||
		!strings.Contains(text, "○ Partial") ||
		!strings.Contains(text, "› ◉ Minimal") ||
		!strings.Contains(text, "enter/→ select · esc cancel") ||
		!strings.Contains(text, "Cancelled.") {
		t.Fatalf("repeat install skipped confirmation:\n%s", text)
	}
	body, err := os.ReadFile(manager)
	if err != nil || string(body) != "existing manager" {
		t.Fatalf("cancelled reinstall changed manager: %q, %v", body, err)
	}
}

func TestInstallerLogsSelectedReinstallMode(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".wago", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "wago"), []byte("manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	tty := filepath.Join(home, "tty")
	if err := os.WriteFile(tty, []byte("\n"), 0o600); err != nil {
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
		t.Fatalf("select reinstall mode: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Reinstall mode: Minimal") {
		t.Fatalf("installer did not log selected reinstall mode:\n%s", output)
	}
}

func TestInstallerFullReinstallRemovesPluginsAndPathSetup(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".wago")
	for _, path := range []string{
		filepath.Join(root, "bin"),
		filepath.Join(root, "versions", "canary"),
		filepath.Join(root, "src"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(root, "bin", "wago"):                        "manager",
		filepath.Join(root, "wago.json"):                          "plugins",
		filepath.Join(root, "wago-lock.json"):                     "lock",
		filepath.Join(root, "src", "go.mod"):                      "source",
		filepath.Join(root, "versions", "canary", "wago-runtime"): "runtime",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	zshrc := filepath.Join(home, ".zshrc")
	pathBlock := "export KEEP=1\n\n# Wago PATH: " + filepath.Join(root, "bin") +
		"\nexport PATH='" + filepath.Join(root, "bin") + "':\"$PATH\"\n"
	if err := os.WriteFile(zshrc, []byte(pathBlock), 0o644); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY=full",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("full reinstall cleanup: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "mode=full plugins=removed") {
		t.Fatalf("full reinstall cleanup output:\n%s", output)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("full reinstall kept Wago root: %v", err)
	}
	config, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(config), "export KEEP=1\n"; got != want {
		t.Fatalf("full reinstall PATH cleanup = %q, want %q", got, want)
	}
}

func TestInstallerPartialReinstallPreservesGlobalPlugins(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".wago")
	data := filepath.Join(root, "data")
	for _, path := range []string{
		filepath.Join(root, "bin"),
		filepath.Join(data, "versions", "canary"),
		filepath.Join(root, "config"),
		filepath.Join(root, "cache", "canary"),
		filepath.Join(root, "src"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(root, "bin", "wago"):                        "manager",
		filepath.Join(root, "wago.json"):                          "plugins",
		filepath.Join(root, "wago-lock.json"):                     "lock",
		filepath.Join(root, "src", "go.mod"):                      "source",
		filepath.Join(root, "config", "active-version"):           "canary",
		filepath.Join(data, "versions", "canary", "wago-runtime"): "runtime",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	zshrc := filepath.Join(home, ".zshrc")
	pathBlock := "# Wago PATH: " + filepath.Join(root, "bin") +
		"\nexport PATH='" + filepath.Join(root, "bin") + "':\"$PATH\"\n"
	if err := os.WriteFile(zshrc, []byte(pathBlock), 0o644); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"WAGO_HOME="+root,
		"WAGO_INTERNAL_REINSTALL_CLEANUP_ONLY=partial",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("partial reinstall cleanup: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "mode=partial plugins=preserved") {
		t.Fatalf("partial reinstall cleanup output:\n%s", output)
	}
	for _, path := range []string{
		filepath.Join(root, "wago.json"),
		filepath.Join(root, "wago-lock.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("partial reinstall removed %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "bin", "wago"),
		filepath.Join(data, "versions"),
		filepath.Join(root, "config"),
		filepath.Join(root, "cache"),
		filepath.Join(root, "src"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("partial reinstall kept %s: %v", path, err)
		}
	}
	if config, err := os.ReadFile(zshrc); err != nil || len(config) != 0 {
		t.Fatalf("partial reinstall PATH cleanup = %q, %v", config, err)
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
	if err := os.WriteFile(filepath.Join(bin, "wago"), []byte("#!/bin/sh\n"), 0o755); err != nil {
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
		!strings.Contains(text, "› ◉ zsh") ||
		!strings.Contains(text, "~/.zshrc  (current)") ||
		!strings.Contains(text, "○ Not now") ||
		!strings.Contains(text, "Adding to PATH: ~/.zshrc") ||
		!strings.Contains(text, "Added Wago to PATH") ||
		!strings.Contains(text, "Install a version with:\n  wago version install") ||
		strings.Contains(text, "source ~/.zshrc") {
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
		!strings.Contains(output, "Adding to PATH: ~/.zshrc") ||
		!strings.Contains(output, "Added Wago to PATH") ||
		!strings.Contains(output, "Wago is ready!") ||
		!strings.Contains(output, "Open a new shell, or run:\n  source ~/.zshrc && wago version install") ||
		strings.Contains(output, "And then, install a version with:") {
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
	if !strings.Contains(output, "PATH already configured") {
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
