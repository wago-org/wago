package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/httpclient"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestLatestChannelRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/wago-org/wago/releases" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"tag_name":"nightly-20260712-deadbee","target_commitish":"deadbee123456789012345678901234567890123"},{"tag_name":"canary-cafef00","target_commitish":"cafef00123456789012345678901234567890123"}]`))
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	got, err := latestChannelRelease("nightly")
	if err != nil || got != "nightly-20260712-deadbee@deadbee123456789012345678901234567890123" {
		t.Fatalf("latestChannelRelease(nightly) = %q, %v", got, err)
	}
}

func TestLatestChannelReleasePaginates(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/wago-org/wago/releases" {
			http.NotFound(w, r)
			return
		}
		requests++
		if r.URL.Query().Get("page") == "1" {
			releases := make([]remoteRelease, releaseDiscoveryPageSize)
			for i := range releases {
				releases[i].TagName = fmt.Sprintf("v1.0.%d", i)
			}
			_ = json.NewEncoder(w).Encode(releases)
			return
		}
		_ = json.NewEncoder(w).Encode([]remoteRelease{{TagName: "nightly-20260812-deadbee", TargetCommitish: "deadbee123456789012345678901234567890123"}})
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	got, err := latestChannelReleaseContext(context.Background(), "nightly")
	if err != nil || got != "nightly-20260812-deadbee@deadbee123456789012345678901234567890123" {
		t.Fatalf("latestChannelReleaseContext = %q, %v", got, err)
	}
	if requests != 2 {
		t.Fatalf("release page requests = %d, want 2", requests)
	}
}

func TestReleaseDiscoveryKeepsLargePagesWithinMetadataLimit(t *testing.T) {
	const releaseCount = 100
	requests := 0
	maximumPageSize := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		pageSize, err := strconv.Atoi(r.URL.Query().Get("per_page"))
		if err != nil || pageSize <= 0 {
			t.Errorf("release page size = %q, want a positive integer", r.URL.Query().Get("per_page"))
			http.Error(w, "invalid page size", http.StatusBadRequest)
			return
		}
		if pageSize > maximumPageSize {
			maximumPageSize = pageSize
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page <= 0 {
			t.Errorf("release page = %q, want a positive integer", r.URL.Query().Get("page"))
			http.Error(w, "invalid page", http.StatusBadRequest)
			return
		}
		first := (page - 1) * pageSize
		last := min(first+pageSize, releaseCount)
		releases := make([]map[string]any, 0, max(last-first, 0))
		for index := first; index < last; index++ {
			releases = append(releases, map[string]any{
				"tag_name": fmt.Sprintf("v1.0.%d", index),
				"body":     strings.Repeat("x", 48<<10),
			})
		}
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	releases, err := fetchReleasesContext(context.Background())
	if err != nil || len(releases) != releaseCount {
		t.Fatalf("fetchReleasesContext = %d releases, %v", len(releases), err)
	}
	if maximumPageSize > 20 {
		t.Fatalf("release page size = %d, want at most 20", maximumPageSize)
	}
	if requests < 2 {
		t.Fatalf("release page requests = %d, want pagination", requests)
	}
}

func TestLatestChannelReleaseUsesLinkPaginationAndSkipsDrafts(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Query().Get("cursor") {
		case "":
			w.Header().Set("Link", fmt.Sprintf(`<%s/repositories/1277210043/releases?cursor=next>; rel="next"`, "http://"+r.Host))
			_ = json.NewEncoder(w).Encode([]remoteRelease{{TagName: "nightly-draft", Draft: true, TargetCommitish: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})
		case "next":
			_ = json.NewEncoder(w).Encode([]remoteRelease{{TagName: "nightly-20260812-deadbee", TargetCommitish: "deadbee123456789012345678901234567890123"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	got, err := latestChannelReleaseContext(context.Background(), "nightly")
	if err != nil || got != "nightly-20260812-deadbee@deadbee123456789012345678901234567890123" {
		t.Fatalf("latestChannelReleaseContext = %q, %v", got, err)
	}
	if requests != 2 {
		t.Fatalf("release page requests = %d, want 2", requests)
	}
}

func TestReleasePaginationRejectsMalformedAndCrossOriginTargets(t *testing.T) {
	for _, test := range []struct {
		name string
		link func(*httptest.Server) string
		want string
	}{
		{name: "malformed", link: func(*httptest.Server) string { return `not-a-url; rel="next"` }, want: "malformed"},
		{name: "cross origin", link: func(*httptest.Server) string {
			return `<https://example.invalid/repos/wago-org/wago/releases?page=2>; rel="next"`
		}, want: "invalid"},
		{name: "different path", link: func(server *httptest.Server) string {
			return `<` + server.URL + `/repos/wago-org/other/releases?page=2>; rel="next"`
		}, want: "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Link", test.link(server))
				_ = json.NewEncoder(w).Encode([]remoteRelease{{TagName: "v1.0.0"}})
			}))
			defer server.Close()
			t.Setenv("WAGO_RELEASE_API", server.URL)
			if _, err := latestChannelReleaseContext(context.Background(), "nightly"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("pagination target error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLatestChannelReleaseRejectsPaginationLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, "http://"+r.Host+r.URL.RequestURI()))
		_ = json.NewEncoder(w).Encode([]remoteRelease{{TagName: "v1.0.0"}})
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	if _, err := latestChannelReleaseContext(context.Background(), "nightly"); err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("channel pagination loop error = %v", err)
	}
}

func TestLatestChannelReleaseRejectsInvalidTargetCommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]remoteRelease{{TagName: "nightly-20260812-deadbee", TargetCommitish: "main"}})
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	if _, err := latestChannelReleaseContext(context.Background(), "nightly"); err == nil || !strings.Contains(err.Error(), "invalid target commit") {
		t.Fatalf("invalid channel target error = %v", err)
	}
}

func TestLatestChannelReleaseCancellationBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/wago-org/wago/releases?page=2>; rel="next"`, "http://"+r.Host))
			_ = json.NewEncoder(w).Encode([]remoteRelease{{TagName: "v1.0.0"}})
			cancel()
			return
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	if _, err := latestChannelReleaseContext(ctx, "nightly"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled channel pagination = %v", err)
	}
	if requests > 2 {
		t.Fatalf("release page requests = %d, want at most 2", requests)
	}
}

func TestRollingChannelWithoutPublishedReleaseUsesCanonicalMainSource(t *testing.T) {
	const sha = "deadbee123456789012345678901234567890123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/wago-org/wago/releases":
			_ = json.NewEncoder(w).Encode([]remoteRelease{{TagName: "v1.0.0"}})
		case "/repos/wago-org/wago/commits/main":
			_ = json.NewEncoder(w).Encode(remoteCommit{SHA: sha})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	resolved, sourceOnly, err := resolveRunnerVersionContext(context.Background(), "nightly", nil)
	if err != nil || !sourceOnly || resolved != "nightly@"+sha {
		t.Fatalf("resolveRunnerVersionContext = %q, %v, %v", resolved, sourceOnly, err)
	}
}

func TestLatestChannelReleaseBoundsPagination(t *testing.T) {
	releases := make([]remoteRelease, releaseDiscoveryPageSize)
	for i := range releases {
		releases[i].TagName = fmt.Sprintf("v1.0.%d", i)
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	if _, err := latestChannelReleaseContext(context.Background(), "nightly"); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("channel pagination error = %v", err)
	}
	if requests != releaseDiscoveryPageLimit {
		t.Fatalf("release page requests = %d, want %d", requests, releaseDiscoveryPageLimit)
	}
}

func TestMainCommitBrowsingPaginatesAndResolvesTip(t *testing.T) {
	const tip = "deadbee123456789012345678901234567890123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/wago-org/wago/commits/main":
			_ = json.NewEncoder(w).Encode(remoteCommit{SHA: tip})
		case "/repos/wago-org/wago/commits":
			page := r.URL.Query().Get("page")
			count := 1
			if page == "1" {
				count = 100
			}
			commits := make([]remoteCommit, count)
			for i := range commits {
				commits[i].SHA = fmt.Sprintf("%040x", i+1)
			}
			_ = json.NewEncoder(w).Encode(commits)
		case "/repos/wago-org/wago/releases":
			count := 1
			if r.URL.Query().Get("page") == "1" {
				count = releaseDiscoveryPageSize
			}
			releases := make([]remoteRelease, count)
			for i := range releases {
				releases[i].TagName = fmt.Sprintf("canary-%07x", i+1)
			}
			_ = json.NewEncoder(w).Encode(releases)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("WAGO_RELEASE_API", srv.URL)

	if got, err := latestMainCommit(); err != nil || got != tip {
		t.Fatalf("latestMainCommit = %q, %v", got, err)
	}
	commits, err := fetchMainCommits()
	if err != nil || len(commits) != 101 {
		t.Fatalf("fetchMainCommits = %d commits, %v", len(commits), err)
	}
	releases, err := fetchReleases()
	if err != nil || len(releases) != releaseDiscoveryPageSize+1 {
		t.Fatalf("fetchReleases = %d releases, %v", len(releases), err)
	}
}

func TestVersionBrowseDiscoveryFetchesOnePageAtATime(t *testing.T) {
	releaseRequests, commitRequests := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/wago-org/wago/releases":
			releaseRequests++
			count := releaseDiscoveryPageSize
			if request.URL.Query().Get("page") == "2" {
				count = 1
			}
			releases := make([]remoteRelease, count)
			for index := range releases {
				releases[index].TagName = fmt.Sprintf("v1.%d.%d", releaseRequests, index)
			}
			_ = json.NewEncoder(writer).Encode(releases)
		case "/repos/wago-org/wago/commits":
			commitRequests++
			count := commitDiscoveryPageSize
			if request.URL.Query().Get("page") == "2" {
				count = 1
			}
			commits := make([]remoteCommit, count)
			for index := range commits {
				commits[index].SHA = fmt.Sprintf("%040x", commitRequests*commitDiscoveryPageSize+index)
			}
			_ = json.NewEncoder(writer).Encode(commits)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_API", server.URL)

	discovery := newVersionBrowseDiscovery()
	if err := discovery.loadReleases(context.Background()); err != nil {
		t.Fatalf("initial release page: %v", err)
	}
	if err := discovery.loadCommits(context.Background()); err != nil {
		t.Fatalf("initial commit page: %v", err)
	}
	if releaseRequests != 1 || commitRequests != 1 {
		t.Fatalf("initial requests = releases %d, commits %d; want one each", releaseRequests, commitRequests)
	}
	if len(discovery.releases) != releaseDiscoveryPageSize || len(discovery.commits) != commitDiscoveryPageSize {
		t.Fatalf("initial history = releases %d, commits %d", len(discovery.releases), len(discovery.commits))
	}
	if !discovery.releasesPager.hasMore() || !discovery.commitsPager.hasMore() {
		t.Fatal("full initial pages did not advertise older history")
	}

	if err := discovery.loadReleases(context.Background()); err != nil {
		t.Fatalf("older release page: %v", err)
	}
	if releaseRequests != 2 || commitRequests != 1 {
		t.Fatalf("release load requests = releases %d, commits %d; want 2, 1", releaseRequests, commitRequests)
	}
	if discovery.releasesPager.hasMore() || len(discovery.releases) != releaseDiscoveryPageSize+1 {
		t.Fatalf("release history after short page = %d, more %v", len(discovery.releases), discovery.releasesPager.hasMore())
	}

	if err := discovery.loadCommits(context.Background()); err != nil {
		t.Fatalf("older commit page: %v", err)
	}
	if releaseRequests != 2 || commitRequests != 2 {
		t.Fatalf("commit load requests = releases %d, commits %d; want 2, 2", releaseRequests, commitRequests)
	}
	if discovery.commitsPager.hasMore() || len(discovery.commits) != commitDiscoveryPageSize+1 {
		t.Fatalf("commit history after short page = %d, more %v", len(discovery.commits), discovery.commitsPager.hasMore())
	}
}

func TestVersionBrowseDiscoveryLaterFailurePreservesHistoryAndCursor(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/wago-org/wago/releases" {
			http.NotFound(writer, request)
			return
		}
		requests++
		if requests == 2 {
			http.Error(writer, "temporary failure", http.StatusBadGateway)
			return
		}
		count := releaseDiscoveryPageSize
		if requests == 3 {
			count = 1
		}
		_ = json.NewEncoder(writer).Encode(make([]remoteRelease, count))
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_API", server.URL)

	discovery := newVersionBrowseDiscovery()
	if err := discovery.loadReleases(context.Background()); err != nil {
		t.Fatalf("initial release page: %v", err)
	}
	address, pages := discovery.releasesPager.address, discovery.releasesPager.pages
	if err := discovery.loadReleases(context.Background()); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("later release failure = %v, want 502", err)
	}
	if len(discovery.releases) != releaseDiscoveryPageSize || discovery.releasesPager.address != address || discovery.releasesPager.pages != pages {
		t.Fatalf("failed page mutated history or cursor: releases %d, address %q, pages %d", len(discovery.releases), discovery.releasesPager.address, discovery.releasesPager.pages)
	}
	if err := discovery.loadReleases(context.Background()); err != nil {
		t.Fatalf("retry older release page: %v", err)
	}
	if requests != 3 || len(discovery.releases) != releaseDiscoveryPageSize+1 || discovery.releasesPager.hasMore() {
		t.Fatalf("retried page = requests %d, releases %d, more %v", requests, len(discovery.releases), discovery.releasesPager.hasMore())
	}
}

func TestVersionBrowseDiscoveryCancellationPreservesHistoryAndCursor(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			_ = json.NewEncoder(writer).Encode(make([]remoteCommit, commitDiscoveryPageSize))
			return
		}
		<-request.Context().Done()
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_API", server.URL)

	discovery := newVersionBrowseDiscovery()
	if err := discovery.loadCommits(context.Background()); err != nil {
		t.Fatalf("initial commit page: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := discovery.loadCommits(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled older commit page = %v", err)
	}
	if len(discovery.commits) != commitDiscoveryPageSize || discovery.commitsPager.page != 1 || !discovery.commitsPager.hasMore() {
		t.Fatalf("canceled page mutated history or cursor: commits %d, page %d, more %v", len(discovery.commits), discovery.commitsPager.page, discovery.commitsPager.hasMore())
	}
}

func TestReleaseDiscoveryBoundsTotalPagination(t *testing.T) {
	releases := make([]remoteRelease, releaseDiscoveryPageSize)
	commits := make([]remoteCommit, 100)
	for index := range releases {
		releases[index].TagName = fmt.Sprintf("v%d.0.0", index)
		commits[index].SHA = fmt.Sprintf("%040x", index+1)
	}
	releaseRequests, commitRequests := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/wago-org/wago/releases":
			releaseRequests++
			_ = json.NewEncoder(writer).Encode(releases)
		case "/repos/wago-org/wago/commits":
			commitRequests++
			_ = json.NewEncoder(writer).Encode(commits)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	t.Setenv("WAGO_RELEASE_API", server.URL)

	if _, err := fetchReleasesContext(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("unbounded release pagination error = %v", err)
	}
	if releaseRequests != releaseDiscoveryPageLimit {
		t.Fatalf("release page requests = %d, want %d", releaseRequests, releaseDiscoveryPageLimit)
	}
	if _, err := fetchMainCommitsContext(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("unbounded commit pagination error = %v", err)
	}
	if commitRequests != discoveryPageLimit {
		t.Fatalf("commit page requests = %d, want %d", commitRequests, discoveryPageLimit)
	}
}

func TestReleaseMetadataIsBoundedAndCancelable(t *testing.T) {
	oldMaximum := releaseMetadataMaximum
	releaseMetadataMaximum = 32
	t.Cleanup(func() { releaseMetadataMaximum = oldMaximum })

	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(strings.Repeat("x", 33)))
	}))
	defer oversized.Close()
	t.Setenv("WAGO_RELEASE_API", oversized.URL)
	if _, err := latestMainCommitContext(context.Background()); !errors.Is(err, httpclient.ErrBodyTooLarge) {
		t.Fatalf("oversized release metadata = %v", err)
	}

	stalled := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer stalled.Close()
	t.Setenv("WAGO_RELEASE_API", stalled.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := latestMainCommitContext(ctx)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled release metadata = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled release metadata request did not return")
	}
}

func TestHTTPGetBytesReportsDownloadProgress(t *testing.T) {
	payload := []byte("progress payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	var current, total int64
	got, err := httpGetBytesProgress(srv.URL, func(c, t int64) {
		current, total = c, t
	})
	if err != nil || string(got) != string(payload) {
		t.Fatalf("httpGetBytesProgress = %q, %v", got, err)
	}
	if current != int64(len(payload)) || total != int64(len(payload)) {
		t.Fatalf("download progress = %d/%d, want %d/%d", current, total, len(payload), len(payload))
	}
}

func TestDownloadBinaryChecksum(t *testing.T) {
	payload := []byte("fake wago binary bytes")
	sum := sha256.Sum256(payload)
	hexsum := hex.EncodeToString(sum[:])
	asset := "wago-runtime-standard-normal-" + runtime.GOOS + "-" + runtime.GOARCH

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.9.0/" + asset, "/nightly/" + asset, "/canary/" + asset:
			w.Write(payload)
		case "/v0.9.0/" + asset + ".sha256", "/nightly/" + asset + ".sha256", "/canary/" + asset + ".sha256":
			w.Write([]byte(hexsum + "  " + asset + "\n"))
		case "/bad/" + asset:
			w.Write(payload)
		case "/bad/" + asset + ".sha256":
			w.Write([]byte("deadbeef  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "wago")
	if err := downloadBinary(srv.URL, canonicalReleaseRef("0.9.0"), wagopaths.ProfileStandard, wagopaths.BuildNormal, dest); err != nil {
		t.Fatalf("downloadBinary: %v", err)
	}

	for _, channel := range []string{"nightly", "canary"} {
		dest := filepath.Join(t.TempDir(), "wago")
		if err := downloadBinary(srv.URL, channel, wagopaths.ProfileStandard, wagopaths.BuildNormal, dest); err != nil {
			t.Fatalf("downloadBinary(%q): %v", channel, err)
		}
		got, err := os.ReadFile(dest)
		if err != nil || string(got) != string(payload) {
			t.Fatalf("downloaded %s content mismatch: %v", channel, err)
		}
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("downloaded content mismatch: %v", err)
	}

	// A checksum mismatch must fail and write nothing.
	badDest := filepath.Join(t.TempDir(), "wago")
	if err := downloadBinary(srv.URL, "bad", wagopaths.ProfileStandard, wagopaths.BuildNormal, badDest); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if _, err := os.Stat(badDest); !os.IsNotExist(err) {
		t.Fatal("checksum mismatch must not write the destination file")
	}
}

func TestVersionAssetsIncludeProfileAndHost(t *testing.T) {
	for _, profile := range wagopaths.Profiles {
		for _, build := range wagopaths.Builds {
			want := "wago-runtime-" + string(profile) + "-" + string(build) + "-" + runtime.GOOS + "-" + runtime.GOARCH
			if got := versionAsset(profile, build); got != want {
				t.Fatalf("versionAsset(%s, %s) = %q, want %q", profile, build, got, want)
			}
		}
	}
	wantManager := "wago-" + runtime.GOOS + "-" + runtime.GOARCH
	if got := managerAsset(); got != wantManager {
		t.Fatalf("managerAsset() = %q, want %q", got, wantManager)
	}
}

func TestCanonicalReleaseRef(t *testing.T) {
	for input, want := range map[string]string{
		"0.2.0":                   "v0.2.0",
		"v0.2.0":                  "v0.2.0",
		"main":                    "main",
		"canary-20260729-deadbee": "canary-20260729-deadbee",
	} {
		if got := canonicalReleaseRef(input); got != want {
			t.Fatalf("canonicalReleaseRef(%q) = %q, want %q", input, got, want)
		}
	}
}
