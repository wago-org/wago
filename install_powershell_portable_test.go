package wago

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPowerShellBootstrapFallsBackToGoForMainWithoutRelease(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh is not available")
	}
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	log := filepath.Join(t.TempDir(), "go.log")
	goCommand := filepath.Join(t.TempDir(), "go")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$WAGO_GO_LOG\"\nprintf 'source installer: %s\\n' \"$WAGO_VERSION\"\n"
	if runtime.GOOS == "windows" {
		goCommand += ".cmd"
		content = "@echo off\r\necho %* > \"%WAGO_GO_LOG%\"\r\necho source installer: %WAGO_VERSION%\r\n"
	}
	if err := os.WriteFile(goCommand, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("pwsh", "-NoLogo", "-NoProfile", "-Command", "-")
	command.Stdin = bytes.NewReader(script)
	command.Env = append(os.Environ(),
		"WAGO_VERSION=main",
		"WAGO_INSTALLER_DEBUG=1",
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
		"WAGO_GO_COMMAND="+goCommand,
		"WAGO_GO_LOG="+log,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("source fallback: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "source installer: main"; got != want {
		t.Fatalf("source fallback output = %q, want %q", got, want)
	}
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(args)), "run github.com/wago-org/wago/cli/wago-installer@main install"; got != want {
		t.Fatalf("go fallback arguments = %q, want %q", got, want)
	}
}
