package version

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/httpclient"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestExecuteSourceCommandCancellation(t *testing.T) {
	if os.Getenv("WAGO_TEST_SOURCE_COMMAND_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := executeSourceCommand(ctx, "", append(os.Environ(), "WAGO_TEST_SOURCE_COMMAND_HELPER=1"), os.Args[0], "-test.run=^TestExecuteSourceCommandCancellation$")
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled source command returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled source command did not stop")
	}
}

func TestSourceCheckoutRejectsNilContext(t *testing.T) {
	var nilContext context.Context
	if _, _, _, err := checkoutWagoSourceInContext(nilContext, t.TempDir(), "v1.2.3", nil); err == nil {
		t.Fatal("source checkout accepted a nil context")
	}
}

func TestSourceCheckoutCancellationStopsGitWithoutArchiveFallback(t *testing.T) {
	old := runSourceCommand
	t.Cleanup(func() { runSourceCommand = old })
	started := make(chan struct{})
	runSourceCommand = func(ctx context.Context, _ string, _ []string, _ string, _ ...string) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	parent := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		temp, _, _, err := checkoutWagoSourceInContext(ctx, parent, "v1.2.3", nil)
		if temp != "" {
			done <- fmt.Errorf("canceled checkout returned temp %q", temp)
			return
		}
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled source checkout = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled source checkout did not return")
	}
	matches, err := filepath.Glob(filepath.Join(parent, ".wago-source-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("canceled source checkout left temporary directories: %v, %v", matches, err)
	}
}

func TestDownloadSourceArchiveIsSizeBounded(t *testing.T) {
	oldMaximum := sourceArchiveMaximum
	sourceArchiveMaximum = 8
	t.Cleanup(func() { sourceArchiveMaximum = oldMaximum })

	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "declared", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Length", "9")
			writer.WriteHeader(http.StatusOK)
		}},
		{name: "chunked", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.(http.Flusher).Flush()
			_, _ = writer.Write([]byte("123456789"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			target := filepath.Join(t.TempDir(), "source.zip")
			if err := downloadSourceArchiveContext(context.Background(), server.URL, target); !errors.Is(err, httpclient.ErrBodyTooLarge) {
				t.Fatalf("oversized source archive = %v", err)
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("oversized source archive left target: %v", err)
			}
		})
	}

	truncated := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "8")
		_, _ = writer.Write([]byte("1234"))
	}))
	defer truncated.Close()
	truncatedTarget := filepath.Join(t.TempDir(), "source.zip")
	if err := downloadSourceArchiveContext(context.Background(), truncated.URL, truncatedTarget); err == nil {
		t.Fatal("truncated source archive was accepted")
	}
	if _, err := os.Stat(truncatedTarget); !os.IsNotExist(err) {
		t.Fatalf("truncated source archive left target: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.(http.Flusher).Flush()
		_, _ = writer.Write([]byte("12345678"))
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "source.zip")
	if err := downloadSourceArchiveContext(context.Background(), server.URL, target); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "12345678" {
		t.Fatalf("exact-limit source archive = %q, %v", data, err)
	}
}

func TestMissingReleaseAssetFallsBackToSource(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv("WAGO_RELEASE_BASE", server.URL)

	old := buildRunnerSource
	t.Cleanup(func() { buildRunnerSource = old })
	var gotRef string
	var gotProfile wagopaths.Profile
	var gotBuild wagopaths.Build
	buildRunnerSource = func(_ context.Context, ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, _ *managerprogress.Progress) error {
		gotRef, gotProfile = ref, profile
		gotBuild = build
		return os.WriteFile(dest, []byte("source runner"), 0o755)
	}

	dest := filepath.Join(t.TempDir(), "runner")
	if err := installRunnerPayload("v9.9.9", wagopaths.ProfileMinimal, wagopaths.BuildTiny, dest, false, nil); err != nil {
		t.Fatal(err)
	}
	if gotRef != "v9.9.9" || gotProfile != wagopaths.ProfileMinimal || gotBuild != wagopaths.BuildTiny {
		t.Fatalf("source fallback = %q %q %q", gotRef, gotProfile, gotBuild)
	}
	if body, err := os.ReadFile(dest); err != nil || string(body) != "source runner" {
		t.Fatalf("installed source runner = %q, %v", body, err)
	}
}

func TestMissingManagerAssetFallsBackToSource(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/wago-org/wago/releases/latest" {
			_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer api.Close()
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(strings.Repeat("missing release asset", 1<<12)))
	}))
	defer releases.Close()
	t.Setenv("WAGO_RELEASE_API", api.URL)
	t.Setenv("WAGO_RELEASE_BASE", releases.URL)

	old := buildManagerSource
	t.Cleanup(func() { buildManagerSource = old })
	var gotRef string
	buildManagerSource = func(_ context.Context, ref, dest string, _ *managerprogress.Progress) error {
		gotRef = ref
		return os.WriteFile(dest, []byte("source manager"), 0o755)
	}

	dest := filepath.Join(t.TempDir(), "manager")
	resolved, err := installManagerUpdate("latest", dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "v9.9.9" || gotRef != "v9.9.9" {
		t.Fatalf("manager update = %q from %q", resolved, gotRef)
	}
	if body, err := os.ReadFile(dest); err != nil || string(body) != "source manager" {
		t.Fatalf("installed source manager = %q, %v", body, err)
	}
}

func TestChecksumMismatchDoesNotBuildFromSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			asset := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, ".sha256"), "/v1.0.0/")
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + asset + "\n"))
			return
		}
		_, _ = w.Write([]byte("runner"))
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_BASE", server.URL)

	old := buildRunnerSource
	t.Cleanup(func() { buildRunnerSource = old })
	called := false
	buildRunnerSource = func(context.Context, string, wagopaths.Profile, wagopaths.Build, string, *managerprogress.Progress) error {
		called = true
		return nil
	}
	err := installRunnerPayload("v1.0.0", wagopaths.ProfileStandard, wagopaths.BuildNormal, filepath.Join(t.TempDir(), "runner"), false, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("install error = %v", err)
	}
	if called {
		t.Fatal("checksum mismatch triggered source fallback")
	}
}

func TestCanaryResolvesLatestMainCommit(t *testing.T) {
	const sha = "deadbee123456789012345678901234567890123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sha":"` + sha + `"}`))
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_API", server.URL)
	ref, sourceOnly, err := resolveRunnerVersion("canary", nil)
	if err != nil || ref != canaryCommitTarget(sha) || sourceOnly {
		t.Fatalf("resolveRunnerVersion = %q, %v, %v", ref, sourceOnly, err)
	}
}

func TestCanaryCommitMissingReleaseBuildsExactSource(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv("WAGO_RELEASE_BASE", server.URL)

	old := buildRunnerSource
	t.Cleanup(func() { buildRunnerSource = old })
	const sha = "deadbee123456789012345678901234567890123"
	target := canaryCommitTarget(sha)
	var gotRef string
	buildRunnerSource = func(_ context.Context, ref string, _ wagopaths.Profile, _ wagopaths.Build, dest string, _ *managerprogress.Progress) error {
		gotRef = ref
		return os.WriteFile(dest, []byte("source runner"), 0o755)
	}
	dest := filepath.Join(t.TempDir(), "runner")
	if err := installRunnerPayload(target, wagopaths.ProfileStandard, wagopaths.BuildNormal, dest, false, nil); err != nil {
		t.Fatal(err)
	}
	if gotRef != target {
		t.Fatalf("source fallback ref = %q, want %q", gotRef, target)
	}
}

func TestRollingReleaseMissingAssetsPreserveExactSourceIdentity(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv("WAGO_RELEASE_BASE", server.URL)

	const sha = "deadbee123456789012345678901234567890123"
	targets := []string{
		"canary@" + sha,
		"nightly-20260812-" + sha + "@" + sha,
	}
	for _, target := range targets {
		t.Run(target[:strings.IndexByte(target, '@')], func(t *testing.T) {
			oldRunner, oldManager := buildRunnerSource, buildManagerSource
			t.Cleanup(func() { buildRunnerSource, buildManagerSource = oldRunner, oldManager })
			var runnerRef, managerRef string
			buildRunnerSource = func(_ context.Context, ref string, _ wagopaths.Profile, _ wagopaths.Build, dest string, _ *managerprogress.Progress) error {
				runnerRef = ref
				return os.WriteFile(dest, []byte("source runner"), 0o755)
			}
			buildManagerSource = func(_ context.Context, ref, dest string, _ *managerprogress.Progress) error {
				managerRef = ref
				return os.WriteFile(dest, []byte("source manager"), 0o755)
			}
			if err := installRunnerPayload(target, wagopaths.ProfileStandard, wagopaths.BuildNormal, filepath.Join(t.TempDir(), "runner"), false, nil); err != nil {
				t.Fatal(err)
			}
			if err := installManagerPayload(target, filepath.Join(t.TempDir(), "manager"), false, nil); err != nil {
				t.Fatal(err)
			}
			if runnerRef != target || managerRef != target {
				t.Fatalf("fallback identities = runner %q manager %q, want %q", runnerRef, managerRef, target)
			}
		})
	}
}

func TestBuildRunnerFromSourceUsesProfileTag(t *testing.T) {
	old := runSourceCommand
	t.Cleanup(func() { runSourceCommand = old })
	var commands []string
	runSourceCommand = func(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		switch name {
		case "git":
			return nil, os.MkdirAll(args[len(args)-1], 0o755)
		case "go":
			output := args[slices.Index(args, "-o")+1]
			return nil, os.WriteFile(output, []byte("built"), 0o755)
		default:
			return nil, errors.New("unexpected command")
		}
	}

	dest := filepath.Join(t.TempDir(), "runner")
	if err := buildRunnerFromSource("v1.2.3", wagopaths.ProfileMinimal, wagopaths.BuildNormal, dest, nil); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.Contains(commands[0], "git clone") ||
		!strings.Contains(commands[0], "--branch v1.2.3") ||
		!strings.Contains(commands[1], "-tags wago_runtime,wago_minimal") ||
		!strings.Contains(commands[1], "./cli/wago") ||
		!strings.Contains(commands[1], "-X main.version=v1.2.3") {
		t.Fatalf("source commands = %#v", commands)
	}
	if body, err := os.ReadFile(dest); err != nil || string(body) != "built" {
		t.Fatalf("built runner = %q, %v", body, err)
	}
}

func TestFinishSourceBuildReplacesExisting(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "wago")
	if err := os.WriteFile(destination, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	temporary, err := atomicfile.CreateTemp(destination)
	if err != nil {
		t.Fatal(err)
	}
	name := temporary.Name()
	if _, err := temporary.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	if err := finishSourceBuild(name, destination, nil, "built"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "new" {
		t.Fatalf("source build destination = %q, %v", data, err)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".wago-atomic-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("source build temporary debris = %v, %v", matches, err)
	}
}

func TestBuildRunnerFromSourceFallsBackToArchiveWithoutGit(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	mod, err := zw.Create("wago-source/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mod.Write([]byte("module github.com/wago-org/wago\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive.Bytes())
	}))
	defer server.Close()
	t.Setenv("WAGO_ARCHIVE_URL", server.URL+"/source.zip")

	old := runSourceCommand
	t.Cleanup(func() { runSourceCommand = old })
	var commands []string
	runSourceCommand = func(_ context.Context, dir string, _ []string, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == "git" {
			return nil, errors.New(`exec: "git": executable file not found in %PATH%`)
		}
		if name != "go" {
			return nil, errors.New("unexpected command")
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			return nil, err
		}
		output := args[slices.Index(args, "-o")+1]
		return nil, os.WriteFile(output, []byte("built"), 0o755)
	}

	dest := filepath.Join(t.TempDir(), "runner")
	if err := buildRunnerFromSource("v1.2.3", wagopaths.ProfileStandard, wagopaths.BuildNormal, dest, nil); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.HasPrefix(commands[0], "git ") || !strings.HasPrefix(commands[1], "go ") {
		t.Fatalf("source commands = %#v", commands)
	}
}

func TestSourceArchiveURLUsesExactCanaryCommit(t *testing.T) {
	t.Setenv("WAGO_ARCHIVE_URL", "")
	t.Setenv("WAGO_RELEASE_API", "https://example.test/api")
	const sha = "deadbee123456789012345678901234567890123"
	got := sourceArchiveURL(canaryCommitTarget(sha))
	want := "https://example.test/api/repos/wago-org/wago/zipball/" + sha
	if got != want {
		t.Fatalf("source archive URL = %q, want %q", got, want)
	}
}

func TestSourceBuildTags(t *testing.T) {
	if sourceBuildTag(wagopaths.ProfileStandard) != "wago_runtime" ||
		sourceBuildTag(wagopaths.ProfileMinimal) != "wago_runtime,wago_minimal" {
		t.Fatal("source build profile tags changed")
	}
}

func TestBuildRunnerFromSourceChecksOutExactCanaryCommit(t *testing.T) {
	old := runSourceCommand
	t.Cleanup(func() { runSourceCommand = old })
	const sha = "deadbee123456789012345678901234567890123"
	var commands []string
	runSourceCommand = func(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == "go" {
			output := args[slices.Index(args, "-o")+1]
			return nil, os.WriteFile(output, []byte("built"), 0o755)
		}
		return nil, nil
	}
	dest := filepath.Join(t.TempDir(), "runner")
	if err := buildRunnerFromSource(canaryCommitTarget(sha), wagopaths.ProfileStandard, wagopaths.BuildNormal, dest, nil); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{
		"git init --quiet",
		"git -C", "remote add origin",
		"fetch --quiet --depth 1 origin " + sha,
		"checkout --quiet --detach FETCH_HEAD",
		"go build", "-X main.version=canary@" + sha,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("source commands missing %q:\n%s", want, joined)
		}
	}
}

func TestSyncInstalledSourceReplacesManagedCheckoutAtExactCanaryCommit(t *testing.T) {
	old := runSourceCommand
	t.Cleanup(func() { runSourceCommand = old })
	const sha = "880e153000000000000000000000000000000000"
	var commands []string
	runSourceCommand = func(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == "git" && len(args) >= 3 && args[0] == "init" {
			source := args[len(args)-1]
			if err := os.MkdirAll(source, 0o755); err != nil {
				return nil, err
			}
			return nil, os.WriteFile(filepath.Join(source, "go.mod"), []byte("module github.com/wago-org/wago\n"), 0o644)
		}
		return nil, nil
	}

	root := t.TempDir()
	dest := filepath.Join(root, "src")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncInstalledSource(canaryCommitTarget(sha), dest, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "old")); !os.IsNotExist(err) {
		t.Fatalf("stale source survived update: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(dest, "go.mod")); err != nil || !strings.Contains(string(body), "github.com/wago-org/wago") {
		t.Fatalf("updated source go.mod = %q, %v", body, err)
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{
		"fetch --quiet --depth 1 origin " + sha,
		"checkout --quiet --detach FETCH_HEAD",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("source update commands missing %q:\n%s", want, joined)
		}
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
	err := publishSourceUsing(source, dest, nil, rename)
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
