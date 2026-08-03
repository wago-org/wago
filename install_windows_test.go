package wago

import (
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

func TestWindowsInstallerDownloadsChannelManagerRelease(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is available on native Windows CI")
	}
	payload := []byte("released windows manager")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	tag := "canary-deadbee"
	target := "windows-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = fmt.Fprintf(w, "[\n  {\n    \"tag_name\": \"canary-stale00\",\n    \"published_at\": \"2026-01-01T00:00:00Z\"\n  },\n  {\n    \"tag_name\": \"%s\",\n    \"published_at\": \"2026-08-02T00:00:00Z\"\n  }\n]\n", tag)
		case "/releases/download/" + tag + "/wago-" + target:
			_, _ = w.Write(payload)
		case "/releases/download/" + tag + "/wago-" + target + ".sha256":
			_, _ = fmt.Fprintf(w, "%s  wago-%s\n", hash, target)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	command := windowsInstallerCommand(t)
	command.Env = append(os.Environ(),
		"WAGO_VERSION=canary",
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL+"/releases",
		"WAGO_INTERNAL_MANAGER_ONLY=1",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Windows release manager download: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "downloaded Wago manager "+tag) || !strings.Contains(string(output), "manager=release tag="+tag) {
		t.Fatalf("Windows release manager output:\n%s", output)
	}
}

func TestWindowsInstallerDownloadsChannelInstallerRelease(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is available on native Windows CI")
	}
	payload := []byte("released windows installer")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	tag := "nightly-deadbee"
	target := "windows-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = fmt.Fprintf(w, "[\n  {\n    \"tag_name\": \"nightly-stale00\",\n    \"published_at\": \"2026-01-01T00:00:00Z\"\n  },\n  {\n    \"tag_name\": \"%s\",\n    \"published_at\": \"2026-08-02T00:00:00Z\"\n  }\n]\n", tag)
		case "/releases/download/" + tag + "/wago-installer-" + target:
			_, _ = w.Write(payload)
		case "/releases/download/" + tag + "/wago-installer-" + target + ".sha256":
			_, _ = fmt.Fprintf(w, "%s  wago-installer-%s\n", hash, target)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	command := windowsInstallerCommand(t)
	command.Env = append(os.Environ(),
		"WAGO_VERSION=nightly",
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL+"/releases",
		"WAGO_INTERNAL_INSTALLER_ONLY=1",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Windows release installer download: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "downloaded Wago installer "+tag) || !strings.Contains(string(output), "installer=release tag="+tag) {
		t.Fatalf("Windows release installer output:\n%s", output)
	}
}

func windowsInstallerCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is available on native Windows CI")
	}
	return exec.Command("cmd.exe", "/D", "/C", "call install.cmd")
}

func TestCmdInstallerDoesNotUsePowerShell(t *testing.T) {
	cmdScript, err := os.ReadFile("install.cmd")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(cmdScript)), "powershell") {
		t.Fatal("CMD installer depends on PowerShell")
	}
}

func TestCmdInstallerParsesReleaseIndexWithoutCommandSubstitution(t *testing.T) {
	cmdScript, err := os.ReadFile("install.cmd")
	if err != nil {
		t.Fatal(err)
	}
	text := string(cmdScript)
	if strings.Contains(text, "in (`findstr") {
		t.Fatal("CMD installer uses FOR /F command substitution, which Wine cmd.exe does not support")
	}
	if !strings.Contains(text, `in ("!tmp_dir!\releases.json")`) {
		t.Fatal("CMD installer does not parse the downloaded release index directly")
	}
}

func TestCmdInstallerRunsFromCurl(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is available on native Windows CI")
	}
	cmdScript, err := os.ReadFile("install.cmd")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/install.cmd":
			_, _ = w.Write(cmdScript)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	launcher := filepath.Join(home, "wago-install.cmd")
	commandLine := "curl.exe -fsSLo \"" + launcher + "\" " + server.URL + "/install.cmd && call \"" + launcher + "\" && del \"" + launcher + "\""
	runner := filepath.Join(home, "run-install.cmd")
	if err := os.WriteFile(runner, []byte("@echo off\r\n"+commandLine+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("cmd.exe", "/D", "/C", "call run-install.cmd")
	command.Dir = home
	command.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"WAGO_BIN_DIR="+filepath.Join(home, ".wago", "bin"),
		"WAGO_DRY_RUN=1",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("CMD curl installer: %v\n%s", err, output)
	}
	want := filepath.Join(home, ".wago", "bin", "wago.exe")
	if !strings.Contains(string(output), want) || !strings.Contains(string(output), "No changes made.") {
		t.Fatalf("CMD installer output missing %q or dry-run result:\n%s", want, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".wago")); !os.IsNotExist(err) {
		t.Fatalf("CMD dry run changed the installation: %v", err)
	}
	if _, err := os.Stat(launcher); !os.IsNotExist(err) {
		t.Fatalf("documented CMD command kept its launcher: %v", err)
	}
}

func TestWindowsInstallerAsksWhereToInstall(t *testing.T) {
	home := t.TempDir()
	command := windowsInstallerCommand(t)
	command.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "WAGO_INSTALL_CHOICE=2", "WAGO_CUSTOM_INSTALL_DIR=~\\tools\\wago", "WAGO_INTERNAL_INSTALL_DIR_ONLY=1", "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Windows install directory prompt: %v\n%s", err, output)
	}
	text := string(output)
	want := filepath.Join(home, "tools", "wago")
	for _, fragment := range []string{"Where should Wago be installed?", "Custom", "Installing to: " + want, "bin=" + want} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("Windows install directory prompt missing %q:\n%s", fragment, text)
		}
	}
}

func TestWindowsInstallerFallsBackToBasicPromptWithoutReleasedTUI(t *testing.T) {
	home := t.TempDir()
	command := windowsInstallerCommand(t)
	command.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "WAGO_INTERNAL_INSTALL_DIR_ONLY=1", "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Windows installer prompt fallback: %v\n%s", err, output)
	}
	want := filepath.Join(home, ".wago", "bin")
	if !strings.Contains(string(output), "Installing to: "+want) || !strings.Contains(string(output), "bin="+want) {
		t.Fatalf("Windows installer prompt fallback output missing %q:\n%s", want, output)
	}
}

func TestWindowsInstallerHonorsReinstallMode(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".wago", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "wago.exe"), []byte("manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := windowsInstallerCommand(t)
	command.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "WAGO_REINSTALL_MODE=minimal", "WAGO_INTERNAL_REINSTALL_CHECK_ONLY=1", "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Windows reinstall prompt: %v\n%s", err, output)
	}
	text := string(output)
	for _, fragment := range []string{"mode=minimal state=preserved"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("Windows reinstall prompt missing %q:\n%s", fragment, text)
		}
	}
}

func TestWindowsInstallerOffersPathSetupWithoutPowerShell(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".wago", "bin")

	run := func(userPath string) string {
		t.Helper()
		command := windowsInstallerCommand(t)
		command.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "WAGO_BIN_DIR="+bin, "WAGO_PATH_SETUP=yes", "WAGO_TEST_USER_PATH="+userPath, "WAGO_INTERNAL_PATH_SETUP_ONLY=1", "NO_COLOR=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Windows PATH setup: %v\n%s", err, output)
		}
		return string(output)
	}

	if output := run(`C:\Windows\System32`); !strings.Contains(output, "Added Wago to PATH") || !strings.Contains(output, "path="+bin+`;C:\Windows\System32`) {
		t.Fatalf("Windows PATH setup output:\n%s", output)
	}
	if output := run(bin + `;C:\Windows\System32`); !strings.Contains(output, "PATH already configured") {
		t.Fatalf("repeat Windows PATH setup output:\n%s", output)
	}
}
