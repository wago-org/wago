//go:build !wago_lean

// The downloader (install / update / list-remote) pulls in net/http,
// encoding/json, and crypto/sha256. It is excluded from the size-optimized
// TinyGo build (-tags wago_lean), which lacks an ordinary host-network
// transport; that build gets the stubs in version_net_stub.go. Version
// management itself (list/use/…) is net-free and ships in every build
// (version_common.go).

package wagocli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago"
)

func vmInstall(d wago.Dirs, ver string) {
	dest := d.VersionBinary(ver)
	existed := false
	if fi, err := os.Stat(dest); err == nil && !fi.IsDir() {
		// A rolling channel (canary/nightly) re-fetches even when present — the
		// name is stable but the build behind it moves. Only an immutable release
		// short-circuits, since re-downloading identical bytes is pointless.
		if !isRollingChannel(ver) {
			fmt.Printf("wago %s already installed\n", ver)
			return
		}
		existed = true
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version install: %v", err)
	}
	var installErr error
	if ver == "canary" {
		installErr = buildCanary(dest)
	} else {
		installErr = downloadBinary(releaseBase(), ver, dest)
	}
	if installErr != nil {
		fatal("version install: %v", installErr)
	}
	verb := "installed"
	if existed {
		verb = "refreshed"
	}
	fmt.Printf("%s wago %s -> %s\n", verb, cyan(ver), dest)
}

func vmInstallRequested(d wago.Dirs, args []string, latest, nightly, canary bool) {
	if len(args) > 1 || (len(args) == 1 && (latest || nightly || canary)) || (latest && (nightly || canary)) || (nightly && canary) {
		fatal("version install: choose one version or channel")
	}
	if len(args) == 0 && !latest && !nightly && !canary {
		vmBrowse(d)
		return
	}
	if latest {
		vmInstall(d, latestRelease())
		return
	}
	if nightly {
		vmInstall(d, "nightly")
		return
	}
	if canary {
		vmInstall(d, "canary")
		return
	}
	vmInstall(d, args[0])
}

func latestRelease() string {
	resp, err := http.Get(releaseAPI() + "/repos/wago-org/wago/releases/latest")
	if err != nil {
		fatal("version latest: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal("version latest: GitHub returned %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil || release.TagName == "" {
		fatal("version latest: invalid GitHub response")
	}
	return strings.TrimPrefix(release.TagName, "v")
}

func vmBrowse(d wago.Dirs) {
	resp, err := http.Get(releaseAPI() + "/repos/wago-org/wago/releases")
	if err != nil {
		fatal("version browse: %v", err)
	}
	defer resp.Body.Close()
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&releases) != nil {
		fatal("version browse: unable to fetch releases")
	}
	choices := []string{"latest", "nightly", "canary"}
	for _, r := range releases {
		if r.TagName != "" {
			choices = append(choices, strings.TrimPrefix(r.TagName, "v"))
		}
	}
	for i, v := range choices {
		fmt.Printf("  %d) %s\n", i+1, v)
	}
	fmt.Print("Install version: ")
	var n int
	if _, err := fmt.Fscan(os.Stdin, &n); err != nil || n < 1 || n > len(choices) {
		fatal("version browse: invalid selection")
	}
	if choices[n-1] == "latest" {
		vmInstall(d, latestRelease())
		return
	}
	vmInstall(d, choices[n-1])
}

// vmUpdate fetches a fresh copy even when the version is already installed.
// downloadBinary writes a sibling temporary file and renames it only after the
// checksum succeeds, so a failed update leaves the installed binary intact.
func vmUpdate(d wago.Dirs, ver string) {
	dest := d.VersionBinary(ver)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version update: %v", err)
	}
	if ver == "canary" {
		if err := buildCanary(dest); err != nil {
			fatal("version update: %v", err)
		}
		fmt.Printf("updated wago %s -> %s\n", cyan(ver), dest)
		return
	}
	if err := downloadBinary(releaseBase(), ver, dest); err != nil {
		fatal("version update: %v", err)
	}
	fmt.Printf("updated wago %s -> %s\n", cyan(ver), dest)
}

// buildCanary clones main and builds the full CLI locally. Canary is deliberately
// not a release artifact: it is the current source tree, compiled with the
// caller's Go toolchain for the caller's platform.
func buildCanary(dest string) error {
	work, err := os.MkdirTemp("", "wago-canary-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	repo := filepath.Join(work, "wago")
	clone := exec.Command("git", "clone", "--depth=1", "--branch", "main", canaryRepo(), repo)
	if out, err := clone.CombinedOutput(); err != nil {
		return fmt.Errorf("clone main: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	tmp := dest + ".tmp"
	defer os.Remove(tmp)
	build := exec.Command("go", "build", "-o", tmp, "./cli/wago")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build canary: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func canaryRepo() string {
	if v := strings.TrimSpace(os.Getenv("WAGO_CANARY_REPO")); v != "" {
		return v
	}
	return "https://github.com/wago-org/wago.git"
}

func vmListRemote() {
	resp, err := http.Get(releaseAPI() + "/repos/wago-org/wago/releases")
	if err != nil {
		fatal("version list-remote: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal("version list-remote: GitHub returned %s", resp.Status)
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		fatal("version list-remote: %v", err)
	}
	if len(releases) == 0 {
		fmt.Println(dim("no releases published"))
		return
	}
	for _, r := range releases {
		fmt.Println(strings.TrimPrefix(r.TagName, "v"))
	}
}

// downloadBinary fetches the linux/amd64 wago binary for ver from baseURL,
// verifies its SHA-256 against the sibling ".sha256" file, and writes it to dest
// (0755). It writes nothing on a checksum mismatch.
func downloadBinary(baseURL, ver, dest string) error {
	asset := versionAsset()
	url := fmt.Sprintf("%s/%s/%s", strings.TrimRight(baseURL, "/"), ver, asset)

	body, err := httpGetBytes(url)
	if err != nil {
		return err
	}
	sumRaw, err := httpGetBytes(url + ".sha256")
	if err != nil {
		return fmt.Errorf("fetch checksum: %w", err)
	}
	want := strings.TrimSpace(string(sumRaw))
	if fields := strings.Fields(want); len(fields) > 0 {
		want = fields[0] // accept "<hash>  <filename>" form
	}
	got := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return fmt.Errorf("checksum mismatch for %s (want %s, got %x)", asset, want, got)
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func versionAsset() string { return "wago-" + runtime.GOOS + "-" + runtime.GOARCH }

func httpGetBytes(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// releaseBase is the base URL for release binary assets, overridable for testing.
func releaseBase() string {
	if v := os.Getenv("WAGO_RELEASE_BASE"); v != "" {
		return v
	}
	return "https://github.com/wago-org/wago/releases/download"
}

// releaseAPI is the GitHub API base, overridable for testing.
func releaseAPI() string {
	if v := os.Getenv("WAGO_RELEASE_API"); v != "" {
		return v
	}
	return "https://api.github.com"
}
