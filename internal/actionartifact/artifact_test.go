package actionartifact

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testCommit = "deadbee123456789012345678901234567890123"

func TestDownloadExecutable(t *testing.T) {
	const (
		tag    = "v0.1.0-canary.gdeadbee"
		target = "linux-amd64"
		asset  = "wago-linux-amd64"
	)
	payload := []byte("manager")
	archive := artifactZip(t, asset, payload, "")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/artifacts":
			if got := request.URL.Query().Get("name"); got != tag+"-"+target {
				t.Errorf("artifact name = %q", got)
			}
			fmt.Fprintf(writer, `{"artifacts":[{"id":7,"name":%q,"expired":false,"created_at":"2026-09-10T00:00:00Z","archive_download_url":%q,"workflow_run":{"head_sha":%q}}]}`,
				tag+"-"+target, server.URL+"/archive", testCommit)
		case "/archive":
			writer.Header().Set("Content-Length", fmt.Sprint(len(archive)))
			_, _ = writer.Write(archive)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "wago")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := DownloadExecutable(context.Background(), Config{CatalogURL: server.URL + "/artifacts", Token: "secret", HTTPClient: server.Client()}, tag, testCommit, target, asset, destination)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded payload = %q, %v", got, err)
	}
	if info, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("downloaded mode = %v", info.Mode().Perm())
	}
}

func TestDownloadCanaryExecutableByCommit(t *testing.T) {
	const target = "linux-amd64"
	const asset = "wago-linux-amd64"
	payload := []byte("tagless canary")
	archive := artifactZip(t, asset, payload, "")
	name := canaryArtifactName(testCommit, target)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/artifacts":
			if got := request.URL.Query().Get("name"); got != name {
				t.Errorf("artifact name = %q", got)
			}
			fmt.Fprintf(writer, `{"artifacts":[{"id":8,"name":%q,"expired":false,"created_at":"2026-09-10T00:00:00Z","archive_download_url":%q,"workflow_run":{"head_sha":%q}}]}`, name, server.URL+"/archive", testCommit)
		case "/archive":
			_, _ = writer.Write(archive)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "wago")
	if err := DownloadCanaryExecutable(context.Background(), Config{CatalogURL: server.URL + "/artifacts", HTTPClient: server.Client()}, testCommit, target, asset, destination); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded payload = %q, %v", got, err)
	}
}

func TestLatestCanaryCommitFindsNewestUsableTargetArtifact(t *testing.T) {
	const target = "darwin-arm64"
	newest := "cafef00123456789012345678901234567890123"
	newerWithoutTarget := "bada550123456789012345678901234567890123"
	failedCanary := "deadc00123456789012345678901234567890123"
	globalCatalogRequested := false
	var artifactRuns []int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/actions/artifacts":
			globalCatalogRequested = true
			items := make([]artifact, 100)
			for index := range items {
				items[index] = artifact{
					ID: int64(index + 1), Name: fmt.Sprintf("unrelated-%d", index),
					ArchiveDownloadURL: "archive",
				}
				items[index].WorkflowRun.HeadSHA = testCommit
			}
			if err := json.NewEncoder(writer).Encode(catalog{Artifacts: items}); err != nil {
				t.Error(err)
			}
		case "/actions/workflows/.github/workflows/canary.yml/runs":
			if request.URL.Query().Get("branch") != "main" || request.URL.Query().Get("status") != "completed" {
				t.Errorf("workflow query = %v", request.URL.Query())
			}
			fmt.Fprintf(writer, `{"total_count":4,"workflow_runs":[
				{"id":5,"head_sha":%q,"head_branch":"main","conclusion":"failure","created_at":"2026-09-10T05:00:00Z"},
				{"id":4,"head_sha":%q,"head_branch":"main","conclusion":"skipped","created_at":"2026-09-10T04:00:00Z"},
				{"id":3,"head_sha":%q,"head_branch":"main","conclusion":"success","created_at":"2026-09-10T03:00:00Z"},
				{"id":2,"head_sha":%q,"head_branch":"main","conclusion":"success","created_at":"2026-09-10T02:00:00Z"}
			]}`, failedCanary, testCommit, newerWithoutTarget, newest)
		case "/actions/runs/5/artifacts":
			artifactRuns = append(artifactRuns, 5)
			fmt.Fprintf(writer, `{"total_count":1,"artifacts":[{"id":9,"name":%q,"expired":false,"archive_download_url":"archive","workflow_run":{"id":5,"head_sha":%q}}]}`,
				canaryArtifactName(failedCanary, target), failedCanary)
		case "/actions/runs/3/artifacts":
			artifactRuns = append(artifactRuns, 3)
			if request.URL.Query().Get("name") != canaryArtifactName(newerWithoutTarget, target) {
				t.Errorf("artifact name = %q", request.URL.Query().Get("name"))
			}
			_, _ = writer.Write([]byte(`{"total_count":0,"artifacts":[]}`))
		case "/actions/runs/2/artifacts":
			artifactRuns = append(artifactRuns, 2)
			if request.URL.Query().Get("name") != canaryArtifactName(newest, target) {
				t.Errorf("artifact name = %q", request.URL.Query().Get("name"))
			}
			fmt.Fprintf(writer, `{"total_count":1,"artifacts":[{"id":8,"name":%q,"expired":false,"archive_download_url":"archive","workflow_run":{"id":2,"head_sha":%q}}]}`,
				canaryArtifactName(newest, target), newest)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	got, err := LatestCanaryCommit(context.Background(), Config{CatalogURL: server.URL + "/actions/artifacts", HTTPClient: server.Client()}, target)
	if err != nil || got != newest {
		t.Fatalf("LatestCanaryCommit = %q, %v", got, err)
	}
	if globalCatalogRequested {
		t.Fatal("canary lookup scanned the global artifact catalog")
	}
	if len(artifactRuns) != 2 || artifactRuns[0] != 3 || artifactRuns[1] != 2 {
		t.Fatalf("artifact workflow runs = %v, want [3 2]", artifactRuns)
	}
}

func TestDownloadExecutablePrefersGitHubCLI(t *testing.T) {
	const (
		tag        = "v0.1.0-canary.gdeadbee"
		target     = "linux-amd64"
		asset      = "wago-linux-amd64"
		repository = "wago-org/wago"
	)
	payload := []byte("manager from gh")
	archiveRequested := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/artifacts":
			fmt.Fprintf(writer, `{"artifacts":[{"id":7,"name":%q,"expired":false,"created_at":"2026-09-10T00:00:00Z","archive_download_url":%q,"workflow_run":{"id":42,"head_sha":%q}}]}`,
				tag+"-"+target, server.URL+"/archive", testCommit)
		case "/archive":
			archiveRequested = true
			http.Error(writer, "direct API must not be used", http.StatusInternalServerError)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	original := downloadWithGitHubCLI
	downloadWithGitHubCLI = func(_ context.Context, gotRepository string, runID int64, name, directory string) error {
		if gotRepository != repository || runID != 42 || name != tag+"-"+target {
			t.Fatalf("gh download = repository %q, run %d, artifact %q", gotRepository, runID, name)
		}
		if err := os.WriteFile(filepath.Join(directory, asset), payload, 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(payload)
		return os.WriteFile(filepath.Join(directory, asset+".sha256"), []byte(fmt.Sprintf("%x  %s\n", sum, asset)), 0o644)
	}
	t.Cleanup(func() { downloadWithGitHubCLI = original })

	destination := filepath.Join(t.TempDir(), "wago")
	err := DownloadExecutable(context.Background(), Config{
		CatalogURL: server.URL + "/artifacts",
		Repository: repository,
		HTTPClient: server.Client(),
	}, tag, testCommit, target, asset, destination)
	if err != nil {
		t.Fatal(err)
	}
	if archiveRequested {
		t.Fatal("direct Actions API archive was requested after successful gh download")
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded payload = %q, %v", got, readErr)
	}
}

func TestDownloadExecutableFallsBackFromGitHubCLIToAPI(t *testing.T) {
	const (
		tag    = "v0.1.0-canary.gdeadbee"
		target = "linux-amd64"
		asset  = "wago-linux-amd64"
	)
	payload := []byte("manager from API")
	archive := artifactZip(t, asset, payload, "")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/artifacts":
			fmt.Fprintf(writer, `{"artifacts":[{"id":7,"name":%q,"expired":false,"created_at":"2026-09-10T00:00:00Z","archive_download_url":%q,"workflow_run":{"id":42,"head_sha":%q}}]}`,
				tag+"-"+target, server.URL+"/archive", testCommit)
		case "/archive":
			_, _ = writer.Write(archive)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	original := downloadWithGitHubCLI
	downloadWithGitHubCLI = func(context.Context, string, int64, string, string) error {
		return errors.New("gh is unavailable")
	}
	t.Cleanup(func() { downloadWithGitHubCLI = original })

	destination := filepath.Join(t.TempDir(), "wago")
	err := DownloadExecutable(context.Background(), Config{
		CatalogURL: server.URL + "/artifacts",
		Repository: "wago-org/wago",
		HTTPClient: server.Client(),
	}, tag, testCommit, target, asset, destination)
	if err != nil {
		t.Fatal(err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded payload = %q, %v", got, readErr)
	}
}

func TestDownloadExecutableRejectsUnusableArtifactWithoutChangingDestination(t *testing.T) {
	const tag = "v0.1.0-canary.gdeadbee"
	for _, test := range []struct {
		name       string
		catalogSHA string
		status     int
		checksum   string
	}{
		{name: "wrong commit", catalogSHA: "cafef00123456789012345678901234567890123", status: http.StatusOK},
		{name: "unauthorized archive", catalogSHA: testCommit, status: http.StatusUnauthorized},
		{name: "bad checksum", catalogSHA: testCommit, status: http.StatusOK, checksum: strings.Repeat("0", 64) + "  wago-linux-amd64\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := artifactZip(t, "wago-linux-amd64", []byte("new"), test.checksum)
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/artifacts":
					fmt.Fprintf(writer, `{"artifacts":[{"id":7,"name":%q,"expired":false,"created_at":"2026-09-10T00:00:00Z","archive_download_url":%q,"workflow_run":{"head_sha":%q}}]}`,
						tag+"-linux-amd64", server.URL+"/archive", test.catalogSHA)
				case "/archive":
					if test.status != http.StatusOK {
						writer.WriteHeader(test.status)
						return
					}
					_, _ = writer.Write(archive)
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()

			destination := filepath.Join(t.TempDir(), "wago")
			if err := os.WriteFile(destination, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			err := DownloadExecutable(context.Background(), Config{CatalogURL: server.URL + "/artifacts", HTTPClient: server.Client()}, tag, testCommit, "linux-amd64", "wago-linux-amd64", destination)
			if err == nil {
				t.Fatal("unusable artifact was accepted")
			}
			if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "old" {
				t.Fatalf("destination = %q, %v after %v", got, readErr, err)
			}
		})
	}
}

func artifactZip(t *testing.T, asset string, payload []byte, checksum string) []byte {
	t.Helper()
	if checksum == "" {
		sum := sha256.Sum256(payload)
		checksum = fmt.Sprintf("%x  %s\n", sum, asset)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, data := range map[string][]byte{asset: payload, asset + ".sha256": []byte(checksum)} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
