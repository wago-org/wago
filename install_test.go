//go:build !windows

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

	"github.com/wago-org/wago/internal/installbootstrap"
)

type bootstrapContractCatalog struct {
	latest   installbootstrap.Release
	releases []installbootstrap.Release
}

func (catalog bootstrapContractCatalog) Latest() (installbootstrap.Release, error) {
	return catalog.latest, nil
}

func (catalog bootstrapContractCatalog) Releases() ([]installbootstrap.Release, error) {
	return append([]installbootstrap.Release(nil), catalog.releases...), nil
}

func TestShellBootstrapMatchesReleaseContract(t *testing.T) {
	catalog := bootstrapContractCatalog{
		latest: installbootstrap.Release{TagName: "v1.2.3", PublishedAt: "2026-08-04T00:00:00Z"},
		releases: []installbootstrap.Release{
			{TagName: "v1.2.3", PublishedAt: "2026-08-02T00:00:00Z"},
			{TagName: "v0.1.0-canary.gaaaaaaa", PublishedAt: "2026-08-01T00:00:00Z"},
			{TagName: "v0.1.0-beta.1", PublishedAt: "2026-08-04T00:00:00Z"},
			{TagName: "v0.1.0-canary.gbbbbbbb", PublishedAt: "2026-08-03T00:00:00Z"},
		},
	}
	for _, version := range []string{"latest", "main", "beta", "v0.1.0-canary.gccccccc"} {
		t.Run(version, func(t *testing.T) {
			wantTag, err := installbootstrap.Resolve(version, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(version, "-canary.g") {
				wantTag = "v0.1.0-beta.1"
			}
			payload := []byte("#!/bin/sh\nexit 0\n")
			hash := fmt.Sprintf("%x", sha256.Sum256(payload))
			asset := "wago-installer-" + runtime.GOOS + "-" + runtime.GOARCH
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/tags":
					_, _ = fmt.Fprint(w, "[\n  {\n    \"name\": \"v0.1.0-canary.gbbbbbbb\",\n    \"commit\": {\n      \"sha\": \"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\"\n    }\n  }\n]\n")
				case "/commits":
					_, _ = fmt.Fprint(w, "[\n  {\n    \"sha\": \"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\"\n  }\n]\n")
				case "/releases/latest":
					_, _ = fmt.Fprintf(w, "{\n  \"tag_name\": %q,\n  \"published_at\": %q\n}\n", catalog.latest.TagName, catalog.latest.PublishedAt)
				case "/releases":
					_, _ = fmt.Fprint(w, "[\n  {\n    \"tag_name\": \"v1.2.3\",\n    \"published_at\": \"2026-08-02T00:00:00Z\"\n  },\n  {\n    \"tag_name\": \"v0.1.0-canary.gaaaaaaa\",\n    \"published_at\": \"2026-08-01T00:00:00Z\"\n  },\n  {\n    \"tag_name\": \"v0.1.0-beta.1\",\n    \"published_at\": \"2026-08-04T00:00:00Z\"\n  },\n  {\n    \"tag_name\": \"v0.1.0-canary.gbbbbbbb\",\n    \"published_at\": \"2026-08-03T00:00:00Z\"\n  }\n]\n")
				case "/download/" + wantTag + "/" + asset:
					_, _ = w.Write(payload)
				case "/download/" + wantTag + "/" + asset + ".sha256":
					_, _ = fmt.Fprintf(w, "%s  %s\n", hash, asset)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			command := exec.Command("sh", "install.sh")
			command.Env = append(os.Environ(),
				"WAGO_VERSION="+version,
				"WAGO_RELEASES_API_URL="+server.URL+"/releases",
				"WAGO_TAGS_API_URL="+server.URL+"/tags",
				"WAGO_COMMITS_API_URL="+server.URL+"/commits",
				"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
			)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("shell bootstrap: %v\n%s", err, output)
			}
		})
	}
}

func TestShellBootstrapDownloadsVerifiesAndExecutesInstaller(t *testing.T) {
	payload := []byte("#!/bin/sh\nprintf 'native installer: %s\\n' \"$WAGO_VERSION\"\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	carrierTag := "v0.1.0-beta.2"
	asset := "wago-installer-" + runtime.GOOS + "-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = fmt.Fprintf(w, "[\n  {\n    \"tag_name\": %q,\n    \"published_at\": \"2026-08-03T00:00:00Z\"\n  }\n]\n", carrierTag)
		case "/download/" + carrierTag + "/" + asset:
			_, _ = w.Write(payload)
		case "/download/" + carrierTag + "/" + asset + ".sha256":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hash, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"WAGO_VERSION=canary",
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run shell bootstrap: %v\n%s", err, output)
	}
	if got, want := string(output), "native installer: canary\n"; got != want {
		t.Fatalf("bootstrap output = %q, want %q", got, want)
	}
}

func TestShellBootstrapStopsCleanlyWhenInstallerIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"WAGO_VERSION=beta",
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("bootstrap unexpectedly succeeded without an installer")
	}
	if text := string(output); !strings.Contains(text, "no published installer") || !strings.Contains(text, "install Go") {
		t.Fatalf("unavailable output:\n%s", output)
	}
}

func TestShellBootstrapFallsBackToGoForMainWithoutRelease(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	log := filepath.Join(t.TempDir(), "go.log")
	goCommand := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(goCommand, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$WAGO_GO_LOG\"\nprintf 'source installer: %s\\n' \"$WAGO_VERSION\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(),
		"WAGO_VERSION=main",
		"WAGO_RELEASES_API_URL="+server.URL+"/releases",
		"WAGO_RELEASE_DOWNLOAD_BASE="+server.URL,
		"WAGO_GO_COMMAND="+goCommand,
		"WAGO_GO_LOG="+log,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("source fallback: %v\n%s", err, output)
	}
	if got, want := string(output), "source installer: main\n"; got != want {
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

func TestShellBootstrapDoesNotBypassBadOfficialChecksum(t *testing.T) {
	payload := []byte("#!/bin/sh\nexit 0\n")
	asset := "wago-installer-" + runtime.GOOS + "-" + runtime.GOARCH
	requestedBeta := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			_, _ = fmt.Fprint(w, `{"tag_name":"v1.0.0","published_at":"2026-08-01T00:00:00Z"}`)
		case "/releases":
			_, _ = fmt.Fprint(w, `[
  {"tag_name":"v1.1.0-beta.1","published_at":"2026-08-02T00:00:00Z"}
]`)
		case "/download/v1.0.0/" + asset:
			_, _ = w.Write(payload)
		case "/download/v1.0.0/" + asset + ".sha256":
			_, _ = fmt.Fprint(w, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  installer\n")
		default:
			if strings.Contains(r.URL.Path, "beta") {
				requestedBeta = true
			}
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
	if err == nil || !strings.Contains(string(output), "could not be verified") {
		t.Fatalf("shell bootstrap checksum result: %v\n%s", err, output)
	}
	if requestedBeta {
		t.Fatal("shell bootstrap requested beta after an official checksum failure")
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

func TestShellBootstrapStartsRefreshedShellOnlyWhenRequested(t *testing.T) {
	tmp := t.TempDir()
	installer := filepath.Join(tmp, "installer")
	shell := filepath.Join(tmp, "shell")
	if err := os.WriteFile(installer, []byte("#!/bin/sh\nprintf 'native installer\\n'\nprintf 'refresh\\n' >\"$WAGO_PATH_REFRESH_FILE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'refreshed shell\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(), "WAGO_INSTALLER="+installer, "SHELL="+shell)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("refresh shell handoff: %v\n%s", err, output)
	}
	if got, want := string(output), "native installer\nrefreshed shell\n"; got != want {
		t.Fatalf("refresh shell output = %q, want %q", got, want)
	}

	if err := os.WriteFile(installer, []byte("#!/bin/sh\nprintf 'native installer\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("sh", "install.sh")
	command.Env = append(os.Environ(), "WAGO_INSTALLER="+installer, "SHELL="+shell)
	output, err = command.CombinedOutput()
	if err != nil {
		t.Fatalf("non-refresh shell handoff: %v\n%s", err, output)
	}
	if got, want := string(output), "native installer\n"; got != want {
		t.Fatalf("non-refresh output = %q, want %q", got, want)
	}
}

func TestBootstrapScriptsKeepInstallationInNativeBinary(t *testing.T) {
	for _, path := range []string{"install.sh", "install.ps1"} {
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

func TestWindowsBootstrapUsesPowerShellOnly(t *testing.T) {
	if _, err := os.Stat("install.cmd"); !os.IsNotExist(err) {
		t.Fatalf("install.cmd must be removed, stat error = %v", err)
	}
	data, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	for _, forbidden := range []string{"install.cmd", "cmd.exe", "comspec"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("PowerShell bootstrap delegates to %q", forbidden)
		}
	}
}

func TestGoInstallBuildsNamedInstallerCommand(t *testing.T) {
	module := exec.Command("go", "list", "-m", "github.com/wago-org/wago/cli/wago-installer")
	module.Dir = "cli/wago-installer"
	output, err := module.CombinedOutput()
	if err != nil {
		t.Fatalf("list wago-installer module: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "github.com/wago-org/wago/cli/wago-installer"; got != want {
		t.Fatalf("wago-installer module = %q, want %q", got, want)
	}

	bin := t.TempDir()
	command := exec.Command("go", "install", ".")
	command.Dir = "cli/wago-installer"
	command.Env = append(os.Environ(), "GOBIN="+bin)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go install wago-installer: %v\n%s", err, output)
	}

	executable := filepath.Join(bin, "wago-installer")
	output, err = exec.Command(executable, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "dev" {
		t.Fatalf("installed wago-installer version = %q, %v; want dev", output, err)
	}
}
