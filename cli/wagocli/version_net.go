//go:build !wago_lean

// The downloader (install / update) pulls in net/http,
// encoding/json, and crypto/sha256. It is excluded from the size-optimized
// TinyGo build (-tags wago_lean), which lacks an ordinary host-network
// transport; that build gets the stubs in version_net_stub.go. Version
// management itself (list/use/…) is net-free and ships in every build
// (version_common.go).

package wagocli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wago-org/wago/internal/wagopaths"
)

func vmInstall(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	installVersion(d, ver, profile, build, true, true)
}

func vmInstallForSwitch(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	installVersion(d, ver, profile, build, false, false)
}

func installVersion(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, offer, showLocation bool) {
	dest := d.RuntimeBinary(ver, string(profile), string(build))
	if installedPath, _, _, installed := installedRuntime(d, ver, profile, build); installed {
		// A rolling channel (canary/nightly) re-fetches even when present — the
		// name is stable but the build behind it moves. Only an immutable release
		// short-circuits, since re-downloading identical bytes is pointless.
		if !isRollingChannel(ver) {
			fmt.Printf("%s %s is already installed\n", cyan("✓"), installedWagoLabel(ver, ver, profile, build))
			if showLocation {
				printDetail(os.Stdout, "location", displayPath(installedPath))
			}
			if offer {
				offerUseInstalled(d, ver, profile, build)
			}
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version install: %v", err)
	}
	progress := newInstallProgress(os.Stderr)
	progress.title("Setting Up")
	resolved, sourceOnly, err := resolveRunnerVersion(ver, progress)
	if err != nil {
		fatal("version install: %v", err)
	}
	if err := installRunnerPayload(resolved, profile, build, dest, sourceOnly, progress); err != nil {
		fatal("version install: %v", err)
	}
	progress.finish("Installed " + installedWagoLabel(ver, resolved, profile, build))
	if showLocation {
		printDetail(progress.out, "location", displayPath(dest))
	}
	if offer {
		offerUseInstalled(d, ver, profile, build)
	}
}

func vmInstallRequested(d wagopaths.Dirs, args []string, latest, nightly, canary bool, profileValue, buildValue string) {
	if len(args) > 1 || (len(args) == 1 && (latest || nightly || canary)) || (latest && (nightly || canary)) || (nightly && canary) {
		fatal("version install: choose one version or channel")
	}
	if len(args) == 0 && !latest && !nightly && !canary {
		vmBrowse(d, profileValue, buildValue)
		return
	}
	if _, err := requestedProfile(profileValue); err != nil {
		fatal("version install: %v", err)
	}
	if _, err := requestedBuild(buildValue); err != nil {
		fatal("version install: %v", err)
	}
	profile, build, ok := chooseInstallVariant(profileValue, buildValue)
	if !ok {
		return
	}
	if latest {
		vmInstall(d, latestRelease(), profile, build)
		return
	}
	if nightly {
		vmInstall(d, "nightly", profile, build)
		return
	}
	if canary {
		vmInstall(d, "canary", profile, build)
		return
	}
	vmInstall(d, args[0], profile, build)
}

func requestedProfile(value string) (wagopaths.Profile, error) {
	if value != "" {
		return wagopaths.ParseProfile(value)
	}
	return wagopaths.ProfileStandard, nil
}

func requestedBuild(value string) (wagopaths.Build, error) {
	if value != "" {
		return wagopaths.ParseBuild(value)
	}
	return wagopaths.BuildNormal, nil
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
	return release.TagName
}

func vmBrowse(d wagopaths.Dirs, profileValue, buildValue string) {
	resp, err := http.Get(releaseAPI() + "/repos/wago-org/wago/releases?per_page=100")
	if err != nil {
		fatal("version browse: %v", err)
	}
	defer resp.Body.Close()
	var releases []remoteRelease
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&releases) != nil {
		fatal("version browse: unable to fetch releases")
	}
	choice, profile, build, ok := chooseInstallPicker(releases, time.Now(), profileValue, buildValue)
	if !ok {
		return
	}
	if choice == "latest" {
		vmInstall(d, latestRelease(), profile, build)
		return
	}
	vmInstall(d, choice, profile, build)
}

// vmUpdate fetches a fresh copy even when the version is already installed.
// downloadBinary writes a sibling temporary file and renames it only after the
// checksum succeeds, so a failed update leaves the installed binary intact.
func vmUpdate(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	dest := d.RuntimeBinary(ver, string(profile), string(build))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version update: %v", err)
	}
	progress := newInstallProgress(os.Stderr)
	progress.title("Updating " + installedWagoLabel(ver, ver, profile, build))
	resolved, sourceOnly, err := resolveRunnerVersion(ver, progress)
	if err != nil {
		fatal("version update: %v", err)
	}
	if err := installRunnerPayload(resolved, profile, build, dest, sourceOnly, progress); err != nil {
		fatal("version update: %v", err)
	}
	progress.finish("Updated " + installedWagoLabel(ver, resolved, profile, build))
}

func resolveRunnerVersion(ver string, progress *installProgress) (resolved string, sourceOnly bool, err error) {
	if !isRollingChannel(ver) {
		return canonicalReleaseRef(ver), false, nil
	}
	if progress != nil {
		progress.begin("resolving release")
	}
	resolved, err = latestChannelRelease(ver)
	if err == nil {
		if progress != nil {
			progress.done("resolved " + releasePickerLabel(resolved))
		}
		return resolved, false, nil
	}
	if errors.Is(err, errNoPublishedRelease) {
		if progress != nil {
			progress.done("no published release; using source")
		}
		return "main", true, nil
	}
	if progress != nil {
		progress.fail("could not resolve release")
	}
	return "", false, err
}

func canonicalReleaseRef(version string) string {
	if version == "" || strings.HasPrefix(version, "v") || channelRelease(version) != "" {
		return version
	}
	major := strings.SplitN(version, ".", 2)[0]
	if _, numeric := atoiOK(major); numeric {
		return "v" + version
	}
	return version
}

func installRunnerPayload(ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, sourceOnly bool, progress *installProgress) error {
	if !sourceOnly {
		err := downloadBinaryWithProgress(releaseBase(), ref, profile, build, dest, progress)
		if err == nil {
			return nil
		}
		if !releaseAssetUnavailable(err) {
			return err
		}
	}
	return buildRunnerSource(ref, profile, build, dest, progress)
}

func latestChannelRelease(channel string) (string, error) {
	resp, err := http.Get(releaseAPI() + "/repos/wago-org/wago/releases?per_page=100")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", err
	}
	prefix := channel + "-"
	for _, release := range releases {
		if strings.HasPrefix(release.TagName, prefix) {
			return release.TagName, nil
		}
	}
	return "", fmt.Errorf("%w: %s", errNoPublishedRelease, channel)
}

var errNoPublishedRelease = errors.New("no published release")

type httpStatusError struct {
	url    string
	code   int
	status string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GET %s: %s", e.url, e.status)
}

func releaseAssetUnavailable(err error) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.code == http.StatusNotFound
}

// downloadBinary fetches the host-platform wago binary for ver from baseURL,
// verifies its SHA-256 against the sibling ".sha256" file, and writes it to dest
// (0755). It writes nothing on a checksum mismatch.
func downloadBinary(baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string) error {
	return downloadBinaryWithProgress(baseURL, ver, profile, build, dest, nil)
}

func downloadBinaryWithProgress(baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *installProgress) error {
	asset := versionAsset(profile, build)
	url := fmt.Sprintf("%s/%s/%s", strings.TrimRight(baseURL, "/"), ver, asset)

	if progress != nil {
		progress.begin("downloading " + asset)
	}
	body, err := httpGetBytesProgress(url, func(current, total int64) {
		if progress != nil {
			progress.percent("downloading "+asset, current, total)
		}
	})
	if err != nil {
		if progress != nil {
			if releaseAssetUnavailable(err) {
				progress.done("release asset unavailable; using source")
			} else {
				progress.fail("download failed")
			}
		}
		return err
	}
	if progress != nil {
		progress.done("downloaded " + asset)
		progress.begin("fetching checksum")
	}
	sumRaw, err := httpGetBytes(url + ".sha256")
	if err != nil {
		if progress != nil {
			if releaseAssetUnavailable(err) {
				progress.done("checksum unavailable; using source")
			} else {
				progress.fail("checksum download failed")
			}
		}
		return fmt.Errorf("fetch checksum: %w", err)
	}
	if progress != nil {
		progress.done("fetched checksum")
		progress.begin("verifying SHA-256")
	}
	want := strings.TrimSpace(string(sumRaw))
	if fields := strings.Fields(want); len(fields) > 0 {
		want = fields[0] // accept "<hash>  <filename>" form
	}
	got := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		if progress != nil {
			progress.fail("checksum verification failed")
		}
		return fmt.Errorf("checksum mismatch for %s (want %s, got %x)", asset, want, got)
	}
	if progress != nil {
		progress.done("verified SHA-256")
		progress.begin("installing executable")
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, 0o755); err != nil {
		if progress != nil {
			progress.fail("could not write executable")
		}
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		if progress != nil {
			progress.fail("could not install executable")
		}
		return err
	}
	if progress != nil {
		progress.done("installed executable")
	}
	return nil
}

func versionAsset(profile wagopaths.Profile, build wagopaths.Build) string {
	return "wago-runtime-" + string(profile) + "-" + string(build) + "-" + runtime.GOOS + "-" + runtime.GOARCH
}

func httpGetBytes(url string) ([]byte, error) {
	return httpGetBytesProgress(url, nil)
}

func httpGetBytesProgress(url string, progress func(current, total int64)) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{url: url, code: resp.StatusCode, status: resp.Status}
	}
	var body bytes.Buffer
	buf := make([]byte, 32*1024)
	var current int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, _ = body.Write(buf[:n])
			current += int64(n)
			if progress != nil {
				progress(current, resp.ContentLength)
			}
		}
		if readErr == io.EOF {
			return body.Bytes(), nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
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
