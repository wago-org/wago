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
	if err := installRunnerPayload("v9.9.9", wagopaths.ProfileLite, wagopaths.BuildTiny, dest, false, nil); err != nil {
		t.Fatal(err)
	}
	if gotRef != "v9.9.9" || gotProfile != wagopaths.ProfileLite || gotBuild != wagopaths.BuildTiny {
		t.Fatalf("source fallback = %q %q %q", gotRef, gotProfile, gotBuild)
	}
	if body, err := os.ReadFile(dest); err != nil || string(body) != "source runner" {
		t.Fatalf("installed source runner = %q, %v", body, err)
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

func TestRollingChannelWithoutReleaseBuildsMain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_API", server.URL)
	ref, sourceOnly, err := resolveRunnerVersion("canary", nil)
	if err != nil || ref != "main" || !sourceOnly {
		t.Fatalf("resolveRunnerVersion = %q, %v, %v", ref, sourceOnly, err)
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
	if err := buildRunnerFromSource("v1.2.3", wagopaths.ProfileLite, wagopaths.BuildNormal, dest, nil); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.Contains(commands[0], "git clone") ||
		!strings.Contains(commands[0], "--branch v1.2.3") ||
		!strings.Contains(commands[1], "-tags wago_lite") ||
		!strings.Contains(commands[1], "-X main.version=v1.2.3") {
		t.Fatalf("source commands = %#v", commands)
	}
	if body, err := os.ReadFile(dest); err != nil || string(body) != "built" {
		t.Fatalf("built runner = %q, %v", body, err)
	}
}

func TestSourceBuildTags(t *testing.T) {
	if sourceBuildTag(wagopaths.ProfileStandard) != "" ||
		sourceBuildTag(wagopaths.ProfileLite) != "wago_lite" ||
		sourceBuildTag(wagopaths.ProfileMinimal) != "wago_minimal" {
		t.Fatal("source build profile tags changed")
	}
}
