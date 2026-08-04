package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallerDryRunPresentation(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("HOME", filepath.Join(string(os.PathSeparator), "home", "wago"))
	t.Setenv("WAGO_VERSION", "canary")
	t.Setenv("WAGO_BIN_DIR", filepath.Join(string(os.PathSeparator), "home", "wago", ".wago", "bin"))
	t.Setenv("WAGO_SRC_DIR", filepath.Join(string(os.PathSeparator), "home", "wago", ".wago", "src"))
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.dryRun = true
	if err := installer.run(); err != nil {
		t.Fatal(err)
	}
	separator := string(os.PathSeparator)
	want := "Welcome to Wago! Let’s get you set up.\n\n" +
		"Install location: ~" + separator + ".wago" + separator + "bin\n\n" +
		"Plan\n  Version  canary\n  Command  ~" + separator + ".wago" + separator + "bin" + separator + executableName("wago") + "\n  Source   ~" + separator + ".wago" + separator + "src\n\n" +
		"Dry run · no changes made.\n"
	if got := output.String(); got != want {
		t.Fatalf("dry-run output:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestInstallerDownloadsNewestChannelManager(t *testing.T) {
	payload := []byte("released manager")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	asset := "wago-" + runtime.GOOS + "-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = fmt.Fprint(w, `[
  {"tag_name":"canary-stale00","published_at":"2026-01-01T00:00:00Z"},
  {"tag_name":"canary-deadbee","published_at":"2026-08-03T00:00:00Z"}
]`)
		case "/download/canary-deadbee/" + asset:
			_, _ = w.Write(payload)
		case "/download/canary-deadbee/" + asset + ".sha256":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hash, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("NO_COLOR", "1")
	t.Setenv("WAGO_VERSION", "canary")
	t.Setenv("WAGO_RELEASES_API_URL", server.URL+"/releases")
	t.Setenv("WAGO_RELEASE_DOWNLOAD_BASE", server.URL)
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.tmpDir = t.TempDir()
	target := filepath.Join(installer.tmpDir, executableName("wago"))
	if err := installer.downloadManager(target); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("manager = %q, %v", got, err)
	}
	if installer.managerTag != "canary-deadbee" || !installer.managerFromRelease {
		t.Fatalf("manager resolution = %q, %v", installer.managerTag, installer.managerFromRelease)
	}
}

func TestUnzipSingleRootRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "source.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("root/../../escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unzipSingleRoot(archive, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("traversing source archive unexpectedly succeeded")
	}
}

func TestUnixPathSetupIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell startup files are Unix-specific")
	}
	home := t.TempDir()
	config := filepath.Join(home, ".zshrc")
	binDir := filepath.Join(home, ".wago", "bin")
	already, err := addPath(binDir, config, "zsh")
	if err != nil || already {
		t.Fatalf("first PATH setup = %v, %v", already, err)
	}
	already, err = addPath(binDir, config, "zsh")
	if err != nil || !already {
		t.Fatalf("second PATH setup = %v, %v", already, err)
	}
	body, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "# Wago PATH:") != 1 {
		t.Fatalf("PATH setup is not idempotent:\n%s", body)
	}
}

func TestInstallerScriptedInstallDirectoryAndReinstallMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WAGO_INSTALL_CHOICE", "2")
	t.Setenv("WAGO_CUSTOM_INSTALL_DIR", "~"+string(os.PathSeparator)+"tools"+string(os.PathSeparator)+"wago")
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	if !installer.chooseInstallDir() {
		t.Fatal("scripted install directory was cancelled")
	}
	if got, want := installer.binDir, filepath.Join(home, "tools", "wago"); got != want {
		t.Fatalf("install directory = %q, want %q", got, want)
	}
	if err := os.MkdirAll(installer.binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installer.binDir, executableName("wago")), []byte("existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_REINSTALL_MODE", "partial")
	mode, ok, err := installer.chooseReinstallMode()
	if err != nil || !ok || mode != "partial" {
		t.Fatalf("reinstall mode = %q, %v, %v", mode, ok, err)
	}
	want := "Reinstall method: Partial\n\n"
	if got := output.String(); got != want {
		t.Fatalf("reinstall output = %q, want %q", got, want)
	}
}

func TestInstallerScriptedPathSetupKeepsStatusDetails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell startup details are Unix-specific")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	t.Setenv("SHELL", filepath.Join(string(os.PathSeparator), "bin", "zsh"))
	t.Setenv("PATH", "")
	t.Setenv("WAGO_PATH_SETUP", "0")
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	ready, configFile := installer.offerPathSetup()
	if !ready || configFile != filepath.Join(home, ".zshrc") {
		t.Fatalf("PATH setup = %v, %q", ready, configFile)
	}
	want := "\nAdd Wago to PATH in ~/.zshrc? Yes\n✓ Added Wago to PATH\n"
	if got := output.String(); got != want {
		t.Fatalf("PATH setup output = %q, want %q", got, want)
	}

	output.Reset()
	t.Setenv("WAGO_PATH_SETUP", "none")
	installer, err = newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.offerPathSetup()
	if got, want := output.String(), "\nAdd Wago to PATH? No\n"; got != want {
		t.Fatalf("skipped PATH output = %q, want %q", got, want)
	}
}

func TestInstallerPromptWordingMatchesWarmFlow(t *testing.T) {
	want := "Add Wago to PATH?"
	if got := pathSetupQuestion(); got != want {
		t.Fatalf("PATH prompt = %q, want %q", got, want)
	}
	for mode, want := range map[string]string{"full": "Full", "partial": "Partial", "minimal": "Minimal"} {
		if got := reinstallLabel(mode); got != want {
			t.Fatalf("reinstall label %q = %q, want %q", mode, got, want)
		}
	}
}

func TestPathRefreshIsOfferedOnlyAfterAddingPath(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("WAGO_REFRESH_PATH", "yes")
	requestFile := filepath.Join(t.TempDir(), "refresh-path")
	t.Setenv("WAGO_PATH_REFRESH_FILE", requestFile)

	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.offerPathRefresh("/home/wago/.zshrc")
	if output.Len() != 0 {
		t.Fatalf("refresh offered before PATH was added: %q", output.String())
	}
	if _, err := os.Stat(requestFile); !os.IsNotExist(err) {
		t.Fatalf("refresh request created before PATH was added: %v", err)
	}

	installer.pathAdded = true
	installer.offerPathRefresh("/home/wago/.zshrc")
	if got, want := output.String(), "\nRefresh PATH now? Yes\n"; got != want {
		t.Fatalf("refresh prompt = %q, want %q", got, want)
	}
	if got, err := os.ReadFile(requestFile); err != nil || string(got) != "/home/wago/.zshrc\n" {
		t.Fatalf("refresh request = %q, %v", got, err)
	}
	if !installer.pathRefresh {
		t.Fatal("accepted PATH refresh was not recorded")
	}
}

func TestInstallerTranscriptParity(t *testing.T) {
	t.Run("fresh install", func(t *testing.T) {
		assertInstallerTranscript(t, false)
	})
	t.Run("reinstall", func(t *testing.T) {
		assertInstallerTranscript(t, true)
	})
}

func assertInstallerTranscript(t *testing.T, reinstall bool) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	t.Setenv("PATH", "")

	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("WAGO_TEST_USER_PATH", `C:\Windows\System32`)
		t.Setenv("WAGO_PATH_SETUP", "yes")
	} else {
		t.Setenv("SHELL", filepath.Join(string(os.PathSeparator), "bin", "zsh"))
		t.Setenv("ZDOTDIR", home)
		t.Setenv("WAGO_PATH_SETUP", "0")
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WAGO_BIN_DIR", filepath.Join(home, ".wago", "bin"))
	t.Setenv("WAGO_PATH_REFRESH_FILE", filepath.Join(home, "refresh-path"))
	t.Setenv("WAGO_REFRESH_PATH", "yes")

	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.welcome()
	installer.installLocation()
	if reinstall {
		installer.field("Reinstall method", "Full")
	}
	fmt.Fprintln(&output)
	statuses := []string{
		"Downloaded Wago manager canary-deadbee",
		"Fetched Wago source",
	}
	if reinstall {
		statuses = append(statuses, "Cleaned existing Wago installation")
	}
	statuses = append(statuses,
		"Installed Wago command",
		"Saved Wago source",
		"Verified installation",
	)
	for _, status := range statuses {
		installer.done(status)
	}
	pathReady, configFile := installer.offerPathSetup()
	installed := filepath.Join(installer.binDir, executableName("wago"))
	if runtime.GOOS != "windows" {
		if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(installed, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		installer.offerCompletions(installed, configFile)
	}
	installer.offerPathRefresh(configFile)
	installer.finish("canary-deadbee", installed, pathReady, configFile)

	separator := string(os.PathSeparator)
	command := "~" + separator + ".wago" + separator + "bin" + separator + executableName("wago")
	want := "Welcome to Wago! Let’s get you set up.\n\n" +
		"Install location: ~" + separator + ".wago" + separator + "bin\n"
	if reinstall {
		want += "Reinstall method: Full\n"
	}
	want += "\n" +
		"✓ Downloaded Wago manager canary-deadbee\n" +
		"✓ Fetched Wago source\n"
	if reinstall {
		want += "✓ Cleaned existing Wago installation\n"
	}
	want +=
		"✓ Installed Wago command\n" +
			"✓ Saved Wago source\n" +
			"✓ Verified installation\n\n"
	if runtime.GOOS == "windows" {
		want += "Add Wago to PATH? Yes\n" +
			"✓ Added Wago to PATH\n\n" +
			"Refresh PATH now? Yes\n\n" +
			"Sweet, Wago canary-deadbee is ready at " + command + "\n"
	} else {
		want += "Add Wago to PATH in ~/.zshrc? Yes\n" +
			"✓ Added Wago to PATH\n\n" +
			"Enable zsh completions? Yes\n" +
			"✓ Enabled zsh completions\n\n" +
			"Refresh PATH now? Yes\n\n" +
			"Sweet, Wago canary-deadbee is ready at " + command + "\n"
	}
	want += "\nThen install the Wago version you want:\n\n" +
		"wago version install\n"
	if got := output.String(); got != want {
		t.Fatalf("installer transcript:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestInstallerWarmFinishAfterPathSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell activation command is Unix-specific")
	}
	home := filepath.Join(string(os.PathSeparator), "home", "wago")
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.pathAdded = true
	installed := filepath.Join(home, ".wago", "bin", "wago")
	installer.finish("canary-deadbee", installed, true, filepath.Join(home, ".zshrc"))
	want := "\nSweet, Wago canary-deadbee is ready at ~/.wago/bin/wago\n\n" +
		"Open a new terminal or run:\n\n" +
		"source ~/.zshrc\n\n" +
		"Then install the Wago version you want:\n\n" +
		"wago version install\n"
	if got := output.String(); got != want {
		t.Fatalf("warm finish:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestUnixPartialAndFullCleanupSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell startup cleanup is Unix-specific")
	}
	home := t.TempDir()
	t.Setenv("ZDOTDIR", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := filepath.Join(home, ".wago")
	binDir := filepath.Join(root, "bin")
	srcDir := filepath.Join(root, "src")
	configDir := filepath.Join(root, "config")
	cacheDir := filepath.Join(root, "cache")
	for _, directory := range []string{binDir, srcDir, filepath.Join(root, "versions"), configDir, cacheDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	plugin := filepath.Join(root, "wago.json")
	if err := os.WriteFile(plugin, []byte("plugins"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "wago"), []byte("manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(home, ".zshrc")
	pathBlock := "export KEEP=1\n\n# Wago PATH: " + binDir + "\nexport PATH='" + binDir + "':\"$PATH\"\n"
	if err := os.WriteFile(configFile, []byte(pathBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cleanPlatformInstall("partial", home, binDir, srcDir, root, configDir, cacheDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plugin); err != nil {
		t.Fatalf("partial cleanup removed global plugins: %v", err)
	}
	for _, path := range []string{filepath.Join(binDir, "wago"), srcDir, filepath.Join(root, "versions"), configDir, cacheDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("partial cleanup kept %s: %v", path, err)
		}
	}
	config, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(config), "export KEEP=1\n"; got != want {
		t.Fatalf("PATH cleanup = %q, want %q", got, want)
	}
	if err := cleanPlatformInstall("full", home, binDir, srcDir, root, configDir, cacheDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("full cleanup kept Wago root: %v", err)
	}
}

func TestInstallerVerificationIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-specific")
	}
	binDir := t.TempDir()
	commandPath := filepath.Join(binDir, "wago")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NO_COLOR", "1")
	t.Setenv("WAGO_VERIFY_TIMEOUT", "1")
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := installer.verify(commandPath); err == nil {
		t.Fatal("hanging command unexpectedly verified")
	}
	if elapsed := time.Since(started); elapsed > 2500*time.Millisecond {
		t.Fatalf("verification took %s", elapsed.Round(time.Millisecond))
	}
}
