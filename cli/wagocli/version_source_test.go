//go:build !wago_lean

package wagocli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/wagopaths"
)

func TestMissingReleaseAssetFallsBackToSource(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv("WAGO_RELEASE_BASE", server.URL)

	old := buildRunnerSource
	t.Cleanup(func() { buildRunnerSource = old })
	var gotRef string
	var gotProfile wagopaths.Profile
	var gotBuild wagopaths.Build
	buildRunnerSource = func(ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, _ *installProgress) error {
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
	releases := httptest.NewServer(http.NotFoundHandler())
	defer releases.Close()
	t.Setenv("WAGO_RELEASE_API", api.URL)
	t.Setenv("WAGO_RELEASE_BASE", releases.URL)

	old := buildManagerSource
	t.Cleanup(func() { buildManagerSource = old })
	var gotRef string
	buildManagerSource = func(ref, dest string, _ *installProgress) error {
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
			_, _ = w.Write([]byte("deadbeef\n"))
			return
		}
		_, _ = w.Write([]byte("runner"))
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_BASE", server.URL)

	old := buildRunnerSource
	t.Cleanup(func() { buildRunnerSource = old })
	called := false
	buildRunnerSource = func(string, wagopaths.Profile, wagopaths.Build, string, *installProgress) error {
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
	buildRunnerSource = func(ref string, _ wagopaths.Profile, _ wagopaths.Build, dest string, _ *installProgress) error {
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

func TestBuildRunnerFromSourceUsesProfileTag(t *testing.T) {
	old := runSourceCommand
	t.Cleanup(func() { runSourceCommand = old })
	var commands []string
	runSourceCommand = func(_ string, _ []string, name string, args ...string) ([]byte, error) {
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
		!strings.Contains(commands[1], "-tags wago_minimal") ||
		!strings.Contains(commands[1], "-X main.version=v1.2.3") {
		t.Fatalf("source commands = %#v", commands)
	}
	if body, err := os.ReadFile(dest); err != nil || string(body) != "built" {
		t.Fatalf("built runner = %q, %v", body, err)
	}
}

func TestSourceBuildTags(t *testing.T) {
	if sourceBuildTag(wagopaths.ProfileStandard) != "" ||
		sourceBuildTag(wagopaths.ProfileMinimal) != "wago_minimal" {
		t.Fatal("source build profile tags changed")
	}
}

func TestBuildRunnerFromSourceChecksOutExactCanaryCommit(t *testing.T) {
	old := runSourceCommand
	t.Cleanup(func() { runSourceCommand = old })
	const sha = "deadbee123456789012345678901234567890123"
	var commands []string
	runSourceCommand = func(_ string, _ []string, name string, args ...string) ([]byte, error) {
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
		"go build", "-X main.version=canary-deadbee",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("source commands missing %q:\n%s", want, joined)
		}
	}
}
