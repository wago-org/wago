//go:build wago_lean

// Lean/TinyGo build: use the host curl executable for the small downloader
// surface. TinyGo has no ordinary host socket transport, while curl provides
// HTTPS, certificate verification, and redirect handling without retaining a
// Go HTTP stack in the release binary.

package wagocli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	installName := canaryCommitVersion(ver)
	dest := d.RuntimeBinary(installName, string(profile), string(build))
	if installedPath, _, _, installed := installedRuntime(d, installName, profile, build); installed {
		if !isRollingChannel(ver) {
			fmt.Printf("%s %s is already installed\n", cyan("✓"), installedWagoLabel(installName, installName, profile, build))
			if showLocation {
				printDetail(os.Stdout, "location", displayPath(installedPath))
			}
			if offer {
				finishVersionInstall(d, installName, profile, build)
			}
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version install: %v", err)
	}
	progress := newInstallProgress(os.Stderr)
	progress.title("Setting Up")
	resolved := ver
	if ver == "canary" {
		progress.begin("resolving main commit")
		var err error
		resolved, err = latestMainCommit()
		if err != nil {
			progress.fail("could not resolve main commit")
			fatal("version install: %v", err)
		}
		resolved = canaryCommitTarget(resolved)
		progress.done("resolved " + canaryCommitVersion(resolved))
	} else if isRollingChannel(ver) {
		progress.begin("resolving release")
		var err error
		resolved, err = latestChannelRelease(ver)
		if err != nil {
			progress.fail("could not resolve release")
			fatal("version install: %v", err)
		}
		progress.done("resolved " + releasePickerLabel(resolved))
	}
	if err := downloadBinaryWithProgress(releaseBase(), canaryCommitVersion(resolved), profile, build, dest, progress); err != nil {
		fatal("version install: %v", err)
	}
	progress.finish("Installed " + installedWagoLabel(installName, canaryCommitVersion(resolved), profile, build))
	if showLocation {
		printDetail(progress.out, "location", displayPath(dest))
	}
	if offer {
		finishVersionInstall(d, installName, profile, build)
	}
}

// vmInstallRequested keeps the lean/TinyGo command surface aligned with the
// standard downloader without retaining net/http in the release binary.
func vmInstallRequested(d wagopaths.Dirs, args []string, latest, nightly, canary bool, profileValue, buildValue string) {
	if len(args) > 1 || (len(args) == 1 && (latest || nightly || canary)) || (latest && (nightly || canary)) || (nightly && canary) {
		fatal("version install: choose one version or channel")
	}
	if _, err := requestedProfile(profileValue); err != nil {
		fatal("version install: %v", err)
	}
	if _, err := requestedBuild(buildValue); err != nil {
		fatal("version install: %v", err)
	}
	if len(args) == 0 && !latest && !nightly && !canary {
		vmBrowse(d, profileValue, buildValue)
		return
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
	release, err := latestStableRelease()
	if err != nil {
		fatal("version latest: %v", err)
	}
	return release
}

func latestStableRelease() (string, error) {
	body, err := curlGetBytes(releaseAPI() + "/repos/wago-org/wago/releases/latest")
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil || release.TagName == "" {
		return "", fmt.Errorf("GitHub returned an invalid latest release")
	}
	return release.TagName, nil
}

func vmBrowse(d wagopaths.Dirs, profileValue, buildValue string) {
	releases, err := fetchReleases()
	if err != nil {
		fatal("version browse: unable to fetch releases: %v", err)
	}
	commits, err := fetchMainCommits()
	if err != nil {
		fatal("version browse: unable to fetch main commits: %v", err)
	}
	choice, profile, build, ok := chooseInstallPicker(releases, commits, time.Now(), profileValue, buildValue)
	if !ok {
		return
	}
	if choice == "latest" {
		vmInstall(d, latestRelease(), profile, build)
		return
	}
	vmInstall(d, choice, profile, build)
}

func fetchReleases() ([]remoteRelease, error) {
	var releases []remoteRelease
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/wago-org/wago/releases?per_page=100&page=%d", releaseAPI(), page)
		body, err := curlGetBytes(url)
		if err != nil {
			return nil, err
		}
		var batch []remoteRelease
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, err
		}
		releases = append(releases, batch...)
		if len(batch) < 100 {
			return releases, nil
		}
	}
}

func vmUpdate(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	dest := d.RuntimeBinary(ver, string(profile), string(build))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version update: %v", err)
	}
	if err := downloadBinary(releaseBase(), releaseDownloadVersion(ver), profile, build, dest); err != nil {
		fatal("version update: %v", err)
	}
	fmt.Printf("updated wago %s -> %s\n", cyan(ver), dest)
	offerUseUpdated(d, ver, profile, build)
}

// releaseDownloadVersion resolves a rolling channel to its newest immutable
// prerelease tag. Stable versions are already immutable release tags.
func releaseDownloadVersion(ver string) string {
	if ver == "canary" {
		sha, err := latestMainCommit()
		if err != nil {
			fatal("version canary: %v", err)
		}
		return canaryCommitVersion(canaryCommitTarget(sha))
	}
	if _, ok := canaryCommitSHA(ver); ok {
		return canaryCommitVersion(ver)
	}
	if !isRollingChannel(ver) {
		return ver
	}
	tag, err := latestChannelRelease(ver)
	if err != nil {
		fatal("version %s: %v", ver, err)
	}
	return tag
}

func latestMainCommit() (string, error) {
	body, err := curlGetBytes(releaseAPI() + "/repos/wago-org/wago/commits/main")
	if err != nil {
		return "", err
	}
	var commit remoteCommit
	if err := json.Unmarshal(body, &commit); err != nil {
		return "", err
	}
	sha := strings.ToLower(commit.SHA)
	if !validCommitSHA(sha) {
		return "", fmt.Errorf("GitHub returned an invalid main commit")
	}
	return sha, nil
}

func fetchMainCommits() ([]remoteCommit, error) {
	var commits []remoteCommit
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/wago-org/wago/commits?sha=main&per_page=100&page=%d", releaseAPI(), page)
		body, err := curlGetBytes(url)
		if err != nil {
			return nil, err
		}
		var batch []remoteCommit
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, err
		}
		commits = append(commits, batch...)
		if len(batch) < 100 {
			return commits, nil
		}
	}
}

func latestChannelRelease(channel string) (string, error) {
	body, err := curlGetBytes(releaseAPI() + "/repos/wago-org/wago/releases?per_page=100")
	if err != nil {
		return "", err
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", err
	}
	prefix := channel + "-"
	for _, release := range releases {
		if strings.HasPrefix(release.TagName, prefix) {
			return release.TagName, nil
		}
	}
	return "", fmt.Errorf("no published %s release", channel)
}

func installManagerUpdate(channel, dest string, progress *installProgress) (string, error) {
	var (
		resolved string
		err      error
	)
	switch channel {
	case "latest":
		if progress != nil {
			progress.begin("resolving latest release")
		}
		resolved, err = latestStableRelease()
	case "canary":
		if progress != nil {
			progress.begin("resolving main commit")
		}
		var sha string
		sha, err = latestMainCommit()
		resolved = canaryCommitVersion(canaryCommitTarget(sha))
	default:
		if progress != nil {
			progress.begin("resolving release")
		}
		resolved, err = latestChannelRelease(channel)
	}
	if err != nil {
		if progress != nil {
			progress.fail("could not resolve release")
		}
		return "", err
	}
	if progress != nil {
		progress.done("resolved " + releasePickerLabel(resolved))
	}
	if err := downloadReleaseAssetWithProgress(releaseBase(), resolved, managerAsset(), dest, progress); err != nil {
		return "", err
	}
	return resolved, nil
}

// downloadBinary verifies the sibling SHA-256 before atomically replacing dest.
func downloadBinary(baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string) error {
	return downloadBinaryWithProgress(baseURL, ver, profile, build, dest, nil)
}

func downloadBinaryWithProgress(baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *installProgress) error {
	asset := versionAsset(profile, build)
	return downloadReleaseAssetWithProgress(baseURL, ver, asset, dest, progress)
}

func downloadReleaseAssetWithProgress(baseURL, ver, asset, dest string, progress *installProgress) error {
	url := fmt.Sprintf("%s/%s/%s", strings.TrimRight(baseURL, "/"), ver, asset)
	if progress != nil {
		progress.begin("downloading " + asset)
	}
	body, err := curlGetBytes(url)
	if err != nil {
		if progress != nil {
			progress.fail("download failed")
		}
		return err
	}
	if progress != nil {
		progress.done("downloaded " + asset)
		progress.begin("fetching checksum")
	}
	sumRaw, err := curlGetBytes(url + ".sha256")
	if err != nil {
		if progress != nil {
			progress.fail("checksum download failed")
		}
		return fmt.Errorf("fetch checksum: %w", err)
	}
	if progress != nil {
		progress.done("fetched checksum")
		progress.begin("verifying SHA-256")
	}
	want := strings.TrimSpace(string(sumRaw))
	if fields := strings.Fields(want); len(fields) > 0 {
		want = fields[0]
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

func managerAsset() string {
	return "wago-" + runtime.GOOS + "-" + runtime.GOARCH
}

// curlGetBytes runs curl without a shell: URL text is always one argument, so a
// requested version cannot become an option or command. --location follows the
// GitHub release-asset redirect and --fail turns non-2xx responses into errors.
func curlGetBytes(url string) ([]byte, error) {
	cmd := exec.Command("curl",
		"--fail", "--location", "--silent", "--show-error",
		"--connect-timeout", "10", "--max-time", "120", "--", url)
	body, err := cmd.Output()
	if err == nil {
		return body, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
		return nil, fmt.Errorf("curl: %s", strings.TrimSpace(string(exitErr.Stderr)))
	}
	return nil, fmt.Errorf("curl: %w", err)
}

func releaseBase() string {
	if v := os.Getenv("WAGO_RELEASE_BASE"); v != "" {
		return v
	}
	return "https://github.com/wago-org/wago/releases/download"
}

func releaseAPI() string {
	if v := os.Getenv("WAGO_RELEASE_API"); v != "" {
		return v
	}
	return "https://api.github.com"
}
