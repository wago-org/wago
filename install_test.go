//go:build !windows

package wago

import (
	"archive/zip"
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

func TestShellBootstrapDownloadsVerifiesAndExecutesInstaller(t *testing.T) {
	payload := []byte("#!/bin/sh\nprintf 'native installer: %s\\n' \"$WAGO_VERSION\"\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	tag := "canary-deadbee"
	asset := "wago-installer-" + runtime.GOOS + "-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = fmt.Fprintf(w, `[{"tag_name":%q,"published_at":"2026-08-03T00:00:00Z"}]`, tag)
		case "/download/" + tag + "/" + asset:
			_, _ = w.Write(payload)
		case "/download/" + tag + "/" + asset + ".sha256":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hash, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"WAGO_VERSION=main",
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run shell bootstrap: %v\n%s", err, output)
	}
	if got, want := string(output), "native installer: main\n"; got != want {
		t.Fatalf("bootstrap output = %q, want %q", got, want)
	}
}

func TestShellBootstrapStopsCleanlyWhenInstallerIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("bootstrap unexpectedly succeeded without an installer")
	}
	if text := string(output); !strings.Contains(text, "installer is unavailable") || !strings.Contains(text, "internet connection") {
		t.Fatalf("unavailable output:\n%s", output)
	}
}

func TestShellBootstrapExplainsLegacyInstallerHandoff(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "legacy-installer")
	if err := os.WriteFile(legacy, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(), "WAGO_INSTALLER="+legacy)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("bootstrap unexpectedly accepted a legacy installer")
	}
	text := string(output)
	if !strings.Contains(text, "installer release predates the native install flow") || !strings.Contains(text, "channel to update") {
		t.Fatalf("legacy installer output:\n%s", output)
	}
}

func TestBootstrapScriptsContainOnlyDeliveryLogic(t *testing.T) {
	for _, path := range []string{"install.sh", "install.cmd"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(data))
		for _, forbidden := range []string{"go build", "git clone", "wago version install", ".bashrc", ".zshrc", "reg.exe add"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s contains installer implementation detail %q", path, forbidden)
			}
		}
	}
}

func TestNativeAndWineDryRunOutputParity(t *testing.T) {
	wine, err := exec.LookPath("wine")
	if err != nil {
		t.Skip("Wine is not installed")
	}
	tmp := t.TempDir()
	unixInstaller := filepath.Join(tmp, "wago-installer")
	windowsInstaller := filepath.Join(tmp, "wago-installer.exe")
	buildInstaller(t, unixInstaller, runtime.GOOS, runtime.GOARCH)
	buildInstaller(t, windowsInstaller, "windows", "amd64")

	unix := exec.Command(unixInstaller)
	unix.Env = append(os.Environ(),
		"NO_COLOR=1", "WAGO_VERSION=parity", "WAGO_DRY_RUN=1",
		"WAGO_BIN_DIR=ROOT/bin", "WAGO_SRC_DIR=ROOT/src",
	)
	unixOutput, err := unix.CombinedOutput()
	if err != nil {
		t.Fatalf("native dry run: %v\n%s", err, unixOutput)
	}

	windowsPath := `Z:` + strings.ReplaceAll(windowsInstaller, "/", `\`)
	windows := exec.Command(wine, "cmd", "/D", "/C", "call install.cmd")
	windows.Env = append(os.Environ(),
		"WINEDEBUG=-all", "NO_COLOR=1", "WAGO_VERSION=parity", "WAGO_DRY_RUN=1",
		"WAGO_BIN_DIR=ROOT/bin", "WAGO_SRC_DIR=ROOT/src", "WAGO_INSTALLER="+windowsPath,
	)
	windowsOutput, err := windows.CombinedOutput()
	if err != nil {
		t.Fatalf("Wine dry run: %v\n%s", err, windowsOutput)
	}
	normalize := func(value string) string {
		value = strings.ReplaceAll(value, "\r", "")
		value = strings.ReplaceAll(value, `\`, "/")
		value = strings.ReplaceAll(value, "wago.exe", "wago")
		return value
	}
	if got, want := normalize(string(windowsOutput)), normalize(string(unixOutput)); got != want {
		t.Fatalf("Wine output differs from native output:\n--- Wine ---\n%s--- native ---\n%s", got, want)
	}
}

func TestWineCmdBootstrapDownloadsVerifiesAndExecutesInstaller(t *testing.T) {
	wine, err := exec.LookPath("wine")
	if err != nil {
		t.Skip("Wine is not installed")
	}
	tmp := t.TempDir()
	installerPath := filepath.Join(tmp, "wago-installer.exe")
	buildInstaller(t, installerPath, "windows", "amd64")
	payload, err := os.ReadFile(installerPath)
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	tag := "canary-bootstrap"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = fmt.Fprintf(w, "[\n  {\n    \"tag_name\": %q,\n    \"published_at\": \"2026-08-03T00:00:00Z\"\n  }\n]\n", tag)
		case "/download/" + tag + "/wago-installer-windows-amd64":
			_, _ = w.Write(payload)
		case "/download/" + tag + "/wago-installer-windows-amd64.sha256":
			_, _ = fmt.Fprintf(w, "%s  wago-installer-windows-amd64\n", hash)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	command := exec.Command(wine, "cmd", "/D", "/C", "call install.cmd")
	command.Env = append(os.Environ(),
		"WINEDEBUG=-all", "NO_COLOR=1", "WAGO_VERSION=canary", "WAGO_DRY_RUN=1",
		"WAGO_BIN_DIR=ROOT\\bin", "WAGO_SRC_DIR=ROOT\\src",
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Wine CMD download bootstrap: %v\n%s", err, output)
	}
	if text := strings.ReplaceAll(string(output), "\r", ""); !strings.Contains(text, "Where should Wago be installed? ROOT\\bin") || !strings.Contains(text, "Dry run · no changes made.") {
		t.Fatalf("Wine CMD download bootstrap output:\n%s", text)
	}
}

func TestWineInstallerCompletesNativeInstallFlow(t *testing.T) {
	wine, err := exec.LookPath("wine")
	if err != nil {
		t.Skip("Wine is not installed")
	}
	tmp := t.TempDir()
	installerPath := filepath.Join(tmp, "wago-installer.exe")
	managerPath := filepath.Join(tmp, "wago-windows-amd64.exe")
	buildInstaller(t, installerPath, "windows", "amd64")
	buildTarget(t, managerPath, "windows", "amd64", "./cli/wago")
	manager, err := os.ReadFile(managerPath)
	if err != nil {
		t.Fatal(err)
	}
	managerHash := fmt.Sprintf("%x", sha256.Sum256(manager))
	sourceArchive := makeSourceArchive(t)
	tag := "canary-winetest"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/" + tag + "/wago-windows-amd64":
			_, _ = w.Write(manager)
		case "/download/" + tag + "/wago-windows-amd64.sha256":
			_, _ = fmt.Fprintf(w, "%s  wago-windows-amd64\n", managerHash)
		case "/source.zip":
			_, _ = w.Write(sourceArchive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := filepath.Join(tmp, "home")
	binDir := filepath.Join(home, ".wago", "bin")
	srcDir := filepath.Join(home, ".wago", "src")
	windowsPath := func(path string) string { return `Z:` + strings.ReplaceAll(path, "/", `\`) }
	command := exec.Command(wine, "cmd", "/D", "/C", "call install.cmd")
	command.Env = append(os.Environ(),
		"WINEDEBUG=-all", "NO_COLOR=1", "WAGO_NO_MODIFY_PATH=1",
		"WAGO_INSTALLER="+windowsPath(installerPath),
		"WAGO_VERSION="+tag,
		"WAGO_BIN_DIR="+windowsPath(binDir),
		"WAGO_SRC_DIR="+windowsPath(srcDir),
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
		"WAGO_ARCHIVE_URL="+server.URL+"/source.zip",
		"WAGO_REPO_URL=Z:\\does-not-exist",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Wine install flow: %v\n%s", err, output)
	}
	text := strings.ReplaceAll(string(output), "\r", "")
	for _, fragment := range []string{
		"Downloaded Wago manager " + tag,
		"Fetched Wago source archive",
		"Verified installation",
		"Sweet, we've installed Wago " + tag,
		"wago version install",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("Wine install output missing %q:\n%s", fragment, text)
		}
	}
	for _, path := range []string{filepath.Join(binDir, "wago.exe"), filepath.Join(srcDir, "go.mod")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Wine install did not create %s: %v", path, err)
		}
	}
}

func buildInstaller(t *testing.T, target, goos, goarch string) {
	t.Helper()
	buildTarget(t, target, goos, goarch, "./cli/installer")
}

func buildTarget(t *testing.T, target, goos, goarch, pkg string) {
	t.Helper()
	command := exec.Command("go", "build", "-o", target, pkg)
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s target %s: %v\n%s", goos, pkg, err, output)
	}
}

func makeSourceArchive(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	entry, err := writer.Create("wago-source/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("module github.com/wago-org/wago\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
