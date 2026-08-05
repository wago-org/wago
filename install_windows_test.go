package wago

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCmdBootstrapDoesNotUsePowerShellOrCommandSubstitution(t *testing.T) {
	data, err := os.ReadFile("install.cmd")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	if strings.Contains(text, "powershell") {
		t.Fatal("CMD bootstrap depends on PowerShell")
	}
	if strings.Contains(text, "in (`") {
		t.Fatal("CMD bootstrap uses FOR /F command substitution, which Wine cmd.exe does not support")
	}
}

func TestCmdBootstrapExecutesNativeInstaller(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is available on native Windows CI")
	}
	tmp := t.TempDir()
	installer := filepath.Join(tmp, "wago-installer.exe")
	command := exec.Command("go", "build", "-o", installer, "./cli/installer")
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build installer: %v\n%s", err, output)
	}
	command = exec.Command("cmd.exe", "/D", "/C", "call install.cmd")
	command.Env = append(os.Environ(),
		"WAGO_INSTALLER="+installer,
		"WAGO_VERSION=parity",
		"WAGO_DRY_RUN=1",
		"WAGO_BIN_DIR=ROOT\\bin",
		"WAGO_SRC_DIR=ROOT\\src",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("CMD bootstrap: %v\n%s", err, output)
	}
	for _, fragment := range []string{"Welcome to Wago!", "Install location: ROOT\\bin", "Plan", "Dry run"} {
		if !strings.Contains(string(output), fragment) {
			t.Fatalf("CMD bootstrap output missing %q:\n%s", fragment, output)
		}
	}
}

func TestPowerShellBootstrapIsQuietAndExecutesCmdBootstrap(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell and cmd.exe are available together on native Windows CI")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/install.cmd" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintln(w, "@echo off")
		_, _ = fmt.Fprintln(w, "if not \"%WAGO_REFRESH_PATH%\"==\"no\" exit /b 9")
		_, _ = fmt.Fprintln(w, "echo PowerShell loader ok")
		_, _ = fmt.Fprintln(w, "if defined WAGO_PATH_REFRESH_FILE echo refresh>\"%WAGO_PATH_REFRESH_FILE%\"")
	}))
	defer server.Close()

	script, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-Command", "-")
	command.Stdin = bytes.NewReader(script)
	command.Env = append(os.Environ(), "WAGO_INSTALL_BASE_URL="+server.URL)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("PowerShell bootstrap: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "PowerShell loader ok"; got != want {
		t.Fatalf("PowerShell bootstrap output = %q, want %q", got, want)
	}
}
