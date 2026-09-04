package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
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

func TestInstallerMovesPayloadsAcrossFilesystems(t *testing.T) {
	downloadRoot := t.TempDir()
	installRoot := t.TempDir()
	crossDevice := errors.New("invalid cross-device link")
	device := func(path string) string {
		path = filepath.Clean(path)
		if path == downloadRoot || strings.HasPrefix(path, downloadRoot+string(os.PathSeparator)) {
			return "download"
		}
		return "install"
	}
	rename := func(source, target string) error {
		if device(source) != device(target) {
			return &os.LinkError{Op: "rename", Old: source, New: target, Err: crossDevice}
		}
		return os.Rename(source, target)
	}
	isCrossDevice := func(err error) bool { return errors.Is(err, crossDevice) }

	managerSource := filepath.Join(downloadRoot, "wago")
	managerTarget := filepath.Join(installRoot, "bin", "wago")
	if err := os.WriteFile(managerSource, []byte("manager"), 0o600); err != nil {
		t.Fatal(err)
	}
	installer := &installer{out: &bytes.Buffer{}, binDir: filepath.Dir(managerTarget), tmpDir: downloadRoot}
	if err := installer.installManagerUsing(managerSource, managerTarget, rename, isCrossDevice); err != nil {
		t.Fatalf("install manager across filesystems: %v", err)
	}
	if payload, err := os.ReadFile(managerTarget); err != nil || string(payload) != "manager" {
		t.Fatalf("installed manager = %q, %v", payload, err)
	}
	if info, err := os.Stat(managerTarget); err != nil {
		t.Fatalf("stat installed manager: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("installed manager mode = %v", info.Mode().Perm())
	}
	if _, err := os.Stat(managerSource); !os.IsNotExist(err) {
		t.Fatalf("staged manager remains: %v", err)
	}

	source := filepath.Join(downloadRoot, "src")
	installer.srcDir = filepath.Join(installRoot, "src")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "new.go"), []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(installer.srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installer.srcDir, "old.go"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installer.saveSourceUsing(source, rename, isCrossDevice); err != nil {
		t.Fatalf("save source across filesystems: %v", err)
	}
	if payload, err := os.ReadFile(filepath.Join(installer.srcDir, "nested", "new.go")); err != nil || string(payload) != "new" {
		t.Fatalf("saved source = %q, %v", payload, err)
	}
	if info, err := os.Stat(filepath.Join(installer.srcDir, "nested", "new.go")); err != nil {
		t.Fatalf("stat saved source: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("saved source mode = %v", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(installer.srcDir, "old.go")); !os.IsNotExist(err) {
		t.Fatalf("old source remains: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("staged source remains: %v", err)
	}
	for _, pattern := range []string{filepath.Join(installRoot, ".wago-source-*"), filepath.Join(filepath.Dir(managerTarget), ".wago-install-*")} {
		if matches, err := filepath.Glob(pattern); err != nil || len(matches) != 0 {
			t.Fatalf("temporary paths for %q = %v, %v", pattern, matches, err)
		}
	}
}

func TestMovePathDoesNotMaskOrdinaryRenameErrors(t *testing.T) {
	source := filepath.Join(t.TempDir(), "wago")
	if err := os.WriteFile(source, []byte("manager"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("permission denied")
	err := movePathUsing(source, filepath.Join(t.TempDir(), "wago"), func(string, string) error {
		return want
	}, func(error) bool { return false })
	if !errors.Is(err, want) {
		t.Fatalf("move error = %v, want %v", err, want)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source changed after ordinary rename error: %v", err)
	}
}

func TestInstallerUsesEmbeddedReleaseIdentityByDefault(t *testing.T) {
	previous := version
	version = "canary@deadbee123456789012345678901234567890123"
	t.Cleanup(func() { version = previous })
	t.Setenv("WAGO_VERSION", "")
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	if installer.version != version {
		t.Fatalf("installer version = %q, want embedded %q", installer.version, version)
	}
}

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
		"Plan\n  Version  canary\n  Command  ~" + separator + ".wago" + separator + "bin" + separator + executableName("wago") + "\n  Source   ~" + separator + ".wago" + separator + "bin" + separator + ".wago-releases\n\n" +
		"Dry run · no changes made.\n"
	if got := output.String(); got != want {
		t.Fatalf("dry-run output:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestInstallerDownloadsExactCanonicalRollingManager(t *testing.T) {
	const sha = "deadbee123456789012345678901234567890123"
	payload := []byte("exact released manager")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	asset := "wago-" + runtime.GOOS + "-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = fmt.Fprintf(w, `[
  {"tag_name":"canary-draft","target_commitish":%q,"published_at":"2026-08-05T00:00:00Z","draft":true},
  {"tag_name":"canary-wrong","target_commitish":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","published_at":"2026-08-04T00:00:00Z"},
  {"tag_name":"canary-exact","target_commitish":%q,"published_at":"2026-08-03T00:00:00Z"}
]`, sha, sha)
		case "/download/canary-exact/" + asset:
			_, _ = w.Write(payload)
		case "/download/canary-exact/" + asset + ".sha256":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hash, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("NO_COLOR", "1")
	t.Setenv("WAGO_VERSION", "canary@"+sha)
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
	if installer.managerTag != "canary-exact" || !installer.managerFromRelease {
		t.Fatalf("manager resolution = %q, %v", installer.managerTag, installer.managerFromRelease)
	}
}

func TestInstallerCanonicalRollingManagerResolutionPaginates(t *testing.T) {
	const sha = "deadbee123456789012345678901234567890123"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("scope") != "installer" || r.URL.Query().Get("per_page") != "100" {
			t.Fatalf("release query = %q", r.URL.RawQuery)
		}
		requests++
		if r.URL.Query().Get("page") == "1" {
			for index := 0; index < 100; index++ {
				if index != 0 {
					_, _ = fmt.Fprint(w, ",")
				}
				if index == 0 {
					_, _ = fmt.Fprint(w, "[")
				}
				_, _ = fmt.Fprintf(w, `{"tag_name":"v1.0.%d"}`, index)
			}
			_, _ = fmt.Fprint(w, "]")
			return
		}
		_, _ = fmt.Fprintf(w, `[{"tag_name":"canary-exact","target_commitish":%q,"published_at":"2026-08-03T00:00:00Z"}]`, sha)
	}))
	defer server.Close()

	i := &installer{releaseAPI: server.URL + "/releases?scope=installer&per_page=1&page=99", httpClient: server.Client()}
	tag, _, err := i.resolveReleaseForTest("canary@" + sha)
	if err != nil || tag != "canary-exact" {
		t.Fatalf("resolve canonical manager = %q, %v", tag, err)
	}
	if requests != 2 {
		t.Fatalf("release requests = %d, want 2", requests)
	}
}

func (i *installer) resolveReleaseForTest(version string) (string, string, error) {
	previous := i.version
	i.version = version
	defer func() { i.version = previous }()
	resolved, base, err := i.resolveRelease()
	return resolved.Tag, base, err
}

func TestInstallerCanonicalRollingGitFetchUsesExactDetachedCommit(t *testing.T) {
	const sha = "deadbee123456789012345678901234567890123"
	previous := runInstallerGit
	var commands [][]string
	runInstallerGit = func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		return nil, nil
	}
	t.Cleanup(func() { runInstallerGit = previous })

	target := filepath.Join(t.TempDir(), "src")
	i := &installer{version: "canary@" + sha, repoURL: "https://example.invalid/wago.git"}
	if output, err := i.fetchSourceWithGit(target, sha); err != nil || output != nil {
		t.Fatalf("fetchSourceWithGit = %q, %v", output, err)
	}
	want := [][]string{
		{"-c", "init.defaultBranch=main", "init", target},
		{"-C", target, "fetch", "--quiet", "--depth", "1", i.repoURL, sha},
		{"-C", target, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
	}
	if fmt.Sprint(commands) != fmt.Sprint(want) {
		t.Fatalf("git commands = %v, want %v", commands, want)
	}
}

func TestInstallerCanonicalRollingArchiveFallbackUsesExactCommit(t *testing.T) {
	const sha = "deadbee123456789012345678901234567890123"
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		w.Header().Set("Content-Type", "application/zip")
		writer := zip.NewWriter(w)
		entry, err := writer.Create("wago-root/go.mod")
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = entry.Write([]byte("module example.invalid/wago\n"))
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	previousGit := runInstallerGit
	previousArchiveURL := installerSourceArchiveURL
	runInstallerGit = func(...string) ([]byte, error) { return []byte("git unavailable"), errors.New("git failed") }
	installerSourceArchiveURL = func(_, ref string) string { return server.URL + "/zipball/" + ref }
	t.Cleanup(func() {
		runInstallerGit = previousGit
		installerSourceArchiveURL = previousArchiveURL
	})
	t.Setenv("WAGO_RELEASE_API", server.URL)
	t.Setenv("WAGO_ARCHIVE_URL", "")
	i := &installer{
		out:        &bytes.Buffer{},
		version:    "nightly@" + sha,
		repoURL:    "https://example.invalid/wago.git",
		archiveURL: server.URL + "/wrong-ref",
		httpClient: server.Client(),
		tmpDir:     t.TempDir(),
	}
	target, err := i.fetchSource()
	if err != nil {
		t.Fatal(err)
	}
	if requested != "/zipball/"+sha {
		t.Fatalf("archive request = %q", requested)
	}
	if i.sourceMethod != "archive" {
		t.Fatalf("source method = %q", i.sourceMethod)
	}
	if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
		t.Fatalf("extracted source: %v", err)
	}
}

func TestInstallerArchiveCancellationCleansTemporaryTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writer := zip.NewWriter(w)
		for _, entry := range []struct {
			name string
			data string
		}{
			{name: "wago-root/go.mod", data: "module example.invalid/wago\n"},
			{name: "wago-root/large", data: strings.Repeat("x", 1<<20)},
		} {
			destination, err := writer.Create(entry.name)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := destination.Write([]byte(entry.data)); err != nil {
				t.Error(err)
				return
			}
		}
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	temporary := t.TempDir()
	target := filepath.Join(temporary, "src")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	i := &installer{
		out:        &bytes.Buffer{},
		httpClient: server.Client(),
		tmpDir:     temporary,
		ctx:        &cancelWhenStagingExists{Context: ctx, cancel: cancel, target: target},
	}
	_, err := i.fetchSourceArchiveAfterGitFailure(target, server.URL, []byte("git unavailable"), errors.New("git failed"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("archive fallback error = %v, want context canceled", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("canceled archive target remains: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(temporary, ".src-extract-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("canceled archive staging paths remain: %v", matches)
	}
}

func TestInstallerArchiveDownloadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		cancel()
		<-request.Context().Done()
	}))
	defer server.Close()
	t.Cleanup(cancel)

	temporary := t.TempDir()
	target := filepath.Join(temporary, "src")
	i := &installer{
		out:        &bytes.Buffer{},
		httpClient: server.Client(),
		tmpDir:     temporary,
		ctx:        ctx,
	}
	_, err := i.fetchSourceArchiveAfterGitFailure(target, server.URL, []byte("git unavailable"), errors.New("git failed"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("archive fallback error = %v, want context canceled", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("canceled archive target remains: %v", err)
	}
}

type cancelWhenStagingExists struct {
	context.Context
	cancel context.CancelFunc
	target string
}

func (ctx *cancelWhenStagingExists) Err() error {
	if err := ctx.Context.Err(); err != nil {
		return err
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(ctx.target), "."+filepath.Base(ctx.target)+"-extract-*"))
	if len(matches) != 0 {
		ctx.cancel()
	}
	return ctx.Context.Err()
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
	t.Setenv("PATH", "")
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

	output.Reset()
	t.Setenv("PATH", installer.binDir)
	installer, err = newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installer.pathAdded = true
	installer.offerPathRefresh("/home/wago/.zshrc")
	if output.Len() != 0 {
		t.Fatalf("refresh offered when Wago is already on PATH: %q", output.String())
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
	want += "\nNow, install the Wago version you want:\n\n" +
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

func TestInstallerFinishWhenPathIsAlreadyReady(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".wago", "bin")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WAGO_BIN_DIR", binDir)
	t.Setenv("PATH", binDir)
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	installer, err := newInstaller(&output)
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(binDir, executableName("wago"))
	installer.finish("canary-deadbee", installed, true, "")
	separator := string(os.PathSeparator)
	want := "\nSweet, Wago canary-deadbee is ready at ~" + separator + ".wago" + separator + "bin" + separator + executableName("wago") + "\n\n" +
		"Now, install the Wago version you want:\n\n" +
		"wago version install\n"
	if got := output.String(); got != want {
		t.Fatalf("ready PATH finish:\n--- got ---\n%s--- want ---\n%s", got, want)
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

func TestSourceRollbackRetainsBackup(t *testing.T) {
	root := t.TempDir()
	source, dest := filepath.Join(root, "new"), filepath.Join(root, "installed")
	for _, dir := range []string{source, dest} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dest, "marker"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	publishErr, restoreErr := errors.New("publish failed"), errors.New("restore failed")
	calls := 0
	rename := func(from, to string) error {
		calls++
		switch calls {
		case 2:
			return publishErr
		case 3:
			return restoreErr
		}
		return os.Rename(from, to)
	}
	err := (&installer{out: &bytes.Buffer{}, srcDir: dest}).saveSourceUsing(source, rename, func(error) bool { return false })
	if !errors.Is(err, publishErr) || !errors.Is(err, restoreErr) {
		t.Fatalf("missing failure cause: %v", err)
	}
	backups, globErr := filepath.Glob(filepath.Join(root, ".wago-source-backup-*", "*"))
	if globErr != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, globErr)
	}
	if !strings.Contains(err.Error(), backups[0]) {
		t.Fatalf("backup path absent from error: %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(backups[0], "marker"))
	if readErr != nil || string(data) != "old" {
		t.Fatalf("backup contents = %q, %v", data, readErr)
	}
}
