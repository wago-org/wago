package wago

import (
	"bytes"
	"crypto/sha256"
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

func TestPowerShellBootstrapExecutesNativeInstaller(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell and Windows executables are available on native Windows CI")
	}
	installer := buildWindowsInstaller(t)
	output, err := runPowerShellBootstrap(t,
		"WAGO_INSTALLER="+installer,
		"WAGO_VERSION=parity",
		"WAGO_DRY_RUN=1",
		`WAGO_BIN_DIR=ROOT\bin`,
		`WAGO_SRC_DIR=ROOT\src`,
		"NO_COLOR=1",
	)
	if err != nil {
		t.Fatalf("PowerShell bootstrap: %v\n%s", err, output)
	}
	for _, fragment := range []string{"Welcome to Wago!", `Install location: ROOT\bin`, "Plan", "Dry run"} {
		if !strings.Contains(string(output), fragment) {
			t.Fatalf("PowerShell bootstrap output missing %q:\n%s", fragment, output)
		}
	}
}

func TestPowerShellBootstrapDownloadsVerifiesAndExecutesInstaller(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell and Windows executables are available on native Windows CI")
	}
	installer := buildWindowsInstaller(t)
	payload, err := os.ReadFile(installer)
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	tag := "v0.1.0-beta.2"
	asset := "wago-installer-windows-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = fmt.Fprintf(w, "[{\"tag_name\":%q,\"published_at\":\"2026-08-03T00:00:00Z\",\"draft\":false}]", tag)
		case "/download/" + tag + "/" + asset:
			_, _ = w.Write(payload)
		case "/download/" + tag + "/" + asset + ".sha256":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hash, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	output, err := runPowerShellBootstrap(t,
		"WAGO_VERSION=beta",
		"WAGO_INSTALLER_DEBUG=1",
		"WAGO_DRY_RUN=1",
		`WAGO_BIN_DIR=ROOT\bin`,
		`WAGO_SRC_DIR=ROOT\src`,
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
		"NO_COLOR=1",
	)
	if err != nil {
		t.Fatalf("PowerShell download bootstrap: %v\n%s", err, output)
	}
	if text := string(output); !strings.Contains(text, `Install location: ROOT\bin`) || !strings.Contains(text, "Dry run") {
		t.Fatalf("PowerShell download bootstrap output:\n%s", text)
	}
}

func buildWindowsInstaller(t *testing.T) string {
	t.Helper()
	installer := filepath.Join(t.TempDir(), "wago-installer.exe")
	command := exec.Command("go", "build", "-o", installer, "./cli/wago-installer")
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build installer: %v\n%s", err, output)
	}
	return installer
}

func runPowerShellBootstrap(t *testing.T, environment ...string) ([]byte, error) {
	t.Helper()
	script, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-Command", "-")
	command.Stdin = bytes.NewReader(script)
	command.Env = append(os.Environ(), environment...)
	return command.CombinedOutput()
}
