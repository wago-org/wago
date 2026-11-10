//go:build !windows

package wago

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShellBootstrapRefreshUsesTerminalInput(t *testing.T) {
	for _, shell := range []string{"sh", "bash"} {
		for _, source := range []string{"installer", "go-fallback"} {
			for _, invocation := range []string{"file", "pipe"} {
				for _, terminal := range []bool{false, true} {
					name := shell + "/" + source + "/" + invocation + "/no-terminal"
					if terminal {
						name = shell + "/" + source + "/" + invocation + "/terminal"
					}
					t.Run(name, func(t *testing.T) {
						testShellBootstrapRefresh(t, shell, source, invocation, terminal)
					})
				}
			}
		}
	}
}

func testShellBootstrapRefresh(t *testing.T, shell, source, invocation string, terminal bool) {
	t.Helper()
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s is unavailable: %v", shell, err)
	}
	bootstrap, err := filepath.Abs("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeScript := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	installer := writeScript("installer", `printf 'native installer\n'
printf 'refresh\n' >"$WAGO_PATH_REFRESH_FILE"
`)
	goCommand := writeScript("go", `printf 'go fallback\n'
printf 'refresh\n' >"$WAGO_PATH_REFRESH_FILE"
`)
	// Select a release with no downloadable assets to reach the fallback with
	// precisely the final three bootstrap lines still unread by piped Bash.
	writeScript("curl", `case "$*" in
  *'/releases?'*)
    while [ "$#" -gt 0 ]; do
      if [ "$1" = -o ]; then
        shift
        printf '[\n {\n "tag_name": "v0.1.0-beta.1",\n "published_at": "2026-08-01T00:00:00Z"\n }\n]\n' >"$1"
        exit 0
      fi
      shift
    done
    ;;
esac
exit 22
`)
	refreshShell := writeScript("refresh-shell", `printf 'refreshed shell: %s\n' "$*"
if [ -t 0 ]; then
  printf 'terminal stdin\n'
else
  printf 'nonterminal stdin\n'
  cat
fi
`)
	runner := writeScript("runner", `SHELL=$BOOTSTRAP_REFRESH_SHELL
export SHELL
if [ "$BOOTSTRAP_INVOCATION" = pipe ]; then
  cat "$BOOTSTRAP_SCRIPT" | "$BOOTSTRAP_SHELL"
else
  "$BOOTSTRAP_SHELL" "$BOOTSTRAP_SCRIPT"
fi
status=$?
printf 'bootstrap status: %s\n' "$status"
exit "$status"
`)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "sh", runner)
	if terminal {
		script, err := exec.LookPath("script")
		if err != nil {
			t.Skipf("PTY test needs script: %v", err)
		}
		switch runtime.GOOS {
		case "linux":
			// Keep paths out of the command string; script's shell expands the
			// quoted environment variable after creating a controlling terminal.
			command = exec.CommandContext(ctx, script, "-q", "-e", "-c", `sh "$BOOTSTRAP_RUNNER"`, os.DevNull)
		case "darwin":
			command = exec.CommandContext(ctx, script, "-q", os.DevNull, "sh", runner)
		default:
			t.Skip("PTY invocation is defined for Linux and macOS")
		}
	} else {
		// An interactive test runner must not accidentally supply /dev/tty.
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	command.WaitDelay = time.Second
	if source == "go-fallback" {
		installer = ""
	}
	command.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SHELL=/bin/sh",
		"BOOTSTRAP_REFRESH_SHELL="+refreshShell,
		"WAGO_VERSION=main",
		"WAGO_INSTALLER="+installer,
		"WAGO_GO_COMMAND="+goCommand,
		"WAGO_RELEASES_API_URL=https://example.invalid/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE=https://example.invalid/releases",
		"BOOTSTRAP_SCRIPT="+bootstrap,
		"BOOTSTRAP_SHELL="+shellPath,
		"BOOTSTRAP_INVOCATION="+invocation,
		"BOOTSTRAP_RUNNER="+runner,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap refresh: %v\n%s", err, output)
	}
	text := strings.ReplaceAll(string(output), "\r", "")
	if !strings.Contains(text, "bootstrap status: 0\n") {
		t.Fatalf("missing successful bootstrap completion:\n%s", output)
	}
	wantSource := "native installer\n"
	if source == "go-fallback" {
		wantSource = "go fallback\n"
	}
	if !strings.Contains(text, wantSource) {
		t.Fatalf("did not exercise %s:\n%s", source, output)
	}
	if terminal {
		if !strings.Contains(text, "refreshed shell: -i\nterminal stdin\n") {
			t.Fatalf("refreshed shell did not receive terminal stdin:\n%s", output)
		}
	} else if strings.Contains(text, "refreshed shell:") {
		t.Fatalf("started a refreshed shell without a terminal:\n%s", output)
	}
	for _, line := range []string{`chmod +x "$tmp/installer"`, `run_installer "$tmp/installer" "$@"`, "start_refreshed_shell"} {
		if strings.Contains(text, line) {
			t.Fatalf("bootstrap source leaked into refreshed shell stdin:\n%s", output)
		}
	}
}
