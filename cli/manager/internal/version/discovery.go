package version

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wago-org/wago/cli/internal/automation"
	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/wagopaths"
)

func latestRelease() string {
	release, err := latestStableRelease()
	if err != nil {
		fatal("version latest: %v", err)
	}
	return release
}

func latestStableRelease() (string, error) {
	if err := automation.RequireOnline("release discovery"); err != nil {
		return "", err
	}
	resp, err := http.Get(releaseAPI() + "/repos/wago-org/wago/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil || release.TagName == "" {
		return "", errors.New("GitHub returned an invalid latest release")
	}
	return release.TagName, nil
}

func vmBrowse(d wagopaths.Dirs, profileValue, buildValue, use string) {
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
		vmInstall(d, latestRelease(), profile, build, use)
		return
	}
	vmInstall(d, choice, profile, build, use)
}

func fetchReleases() ([]remoteRelease, error) {
	if err := automation.RequireOnline("release discovery"); err != nil {
		return nil, err
	}
	var releases []remoteRelease
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/wago-org/wago/releases?per_page=100&page=%d", releaseAPI(), page)
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub returned %s", resp.Status)
		}
		var batch []remoteRelease
		decodeErr := json.NewDecoder(resp.Body).Decode(&batch)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		releases = append(releases, batch...)
		if len(batch) < 100 {
			return releases, nil
		}
	}
}

// vmUpdate resolves the moving channel before touching the installed runtime.
// Matching commits are left intact unless force is set; replacements use a
// sibling temporary file so a failed update preserves the installed binary.
var (
	resolveUpdateRunnerVersion = resolveRunnerVersion
	installUpdateRunnerPayload = installRunnerPayload
)

func vmUpdate(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, use string, force bool) {
	dest := d.RuntimeBinary(ver, string(profile), string(build))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version update: %v", err)
	}
	progress := managerprogress.NewProgress(os.Stderr)
	progress.Title("Updating " + installedWagoLabel(ver, ver, profile, build))
	resolved, sourceOnly, err := resolveUpdateRunnerVersion(ver, progress)
	if err != nil {
		fatal("version update: %v", err)
	}
	if !force && installedCommitMatches(dest, resolved) {
		progress.Finish(installedWagoLabel(ver, resolved, profile, build) + " is already up to date")
		offerUseUpdated(d, ver, profile, build, use)
		return
	}
	if err := installUpdateRunnerPayload(resolved, profile, build, dest, sourceOnly, progress); err != nil {
		fatal("version update: %v", err)
	}
	progress.Finish("Updated " + installedWagoLabel(ver, resolved, profile, build))
	offerUseUpdated(d, ver, profile, build, use)
}

func installedCommitMatches(path, resolved string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	installed := RuntimeRelease(path, "")
	return sameRelease(installed, resolved)
}

func sameRelease(installed, resolved string) bool {
	if installed != "" && installed == resolved {
		return true
	}
	installedCommit, resolvedCommit := commitFromVersion(installed), commitFromVersion(resolved)
	return installedCommit != "" && resolvedCommit != "" &&
		(strings.HasPrefix(installedCommit, resolvedCommit) || strings.HasPrefix(resolvedCommit, installedCommit))
}

func commitFromVersion(version string) string {
	if sha, ok := canaryCommitSHA(version); ok {
		return sha
	}
	if channelRelease(version) == "" {
		return ""
	}
	parts := strings.Split(version, "-")
	commit := parts[len(parts)-1]
	if len(commit) < 7 {
		return ""
	}
	for _, character := range strings.ToLower(commit) {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return ""
		}
	}
	return strings.ToLower(commit)
}

func resolveRunnerVersion(ver string, progress *managerprogress.Progress) (resolved string, sourceOnly bool, err error) {
	if ver == "canary" {
		if progress != nil {
			progress.Begin("resolving main commit")
		}
		sha, resolveErr := latestMainCommit()
		if resolveErr != nil {
			if progress != nil {
				progress.Fail("could not resolve main commit")
			}
			return "", false, resolveErr
		}
		resolved = canaryCommitTarget(sha)
		if progress != nil {
			progress.Done("resolved " + canaryCommitVersion(resolved))
		}
		return resolved, false, nil
	}
	if !isRollingChannel(ver) {
		return canonicalReleaseRef(ver), false, nil
	}
	if progress != nil {
		progress.Begin("resolving release")
	}
	resolved, err = latestChannelRelease(ver)
	if err == nil {
		if progress != nil {
			progress.Done("resolved " + releasePickerLabel(resolved))
		}
		return resolved, false, nil
	}
	if errors.Is(err, errNoPublishedRelease) {
		if progress != nil {
			progress.Done("no published release; using source")
		}
		return "main", true, nil
	}
	if progress != nil {
		progress.Fail("could not resolve release")
	}
	return "", false, err
}

func canonicalReleaseRef(version string) string {
	if version == "" || strings.HasPrefix(version, "v") || channelRelease(version) != "" {
		return version
	}
	major := strings.SplitN(version, ".", 2)[0]
	if _, numeric := ParseNumeric(major); numeric {
		return "v" + version
	}
	return version
}

func installRunnerPayload(ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, sourceOnly bool, progress *managerprogress.Progress) error {
	if !sourceOnly {
		err := downloadBinaryWithProgress(releaseBase(), canaryCommitVersion(ref), profile, build, dest, progress)
		if err == nil {
			return nil
		}
		if !releaseAssetUnavailable(err) {
			return err
		}
	}
	return buildRunnerSource(ref, profile, build, dest, progress)
}

func installManagerUpdate(channel, dest string, progress *managerprogress.Progress) (string, error) {
	resolved, sourceOnly, err := resolveManagerUpdate(channel, progress)
	if err != nil {
		return "", err
	}
	if err := installManagerPayload(resolved, dest, sourceOnly, progress); err != nil {
		return "", err
	}
	return resolved, nil
}

func resolveManagerUpdate(channel string, progress *managerprogress.Progress) (resolved string, sourceOnly bool, err error) {
	if channel == "latest" {
		if progress != nil {
			progress.Begin("resolving latest release")
		}
		resolved, err = latestStableRelease()
		if err == nil && progress != nil {
			progress.Done("resolved " + releasePickerLabel(resolved))
		}
	} else {
		resolved, sourceOnly, err = resolveRunnerVersion(channel, progress)
	}
	if err != nil {
		return "", false, err
	}
	return resolved, sourceOnly, nil
}

func installManagerPayload(resolved, dest string, sourceOnly bool, progress *managerprogress.Progress) error {
	if !sourceOnly {
		err := downloadReleaseAssetWithProgress(
			releaseBase(),
			canaryCommitVersion(resolved),
			managerAsset(),
			dest,
			progress,
		)
		if err == nil {
			return nil
		}
		if !releaseAssetUnavailable(err) {
			return err
		}
	}
	if err := buildManagerSource(resolved, dest, progress); err != nil {
		return err
	}
	return nil
}

func latestMainCommit() (string, error) {
	if err := automation.RequireOnline("main commit discovery"); err != nil {
		return "", err
	}
	resp, err := http.Get(releaseAPI() + "/repos/wago-org/wago/commits/main")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var commit remoteCommit
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
		return "", err
	}
	if !validCommitSHA(strings.ToLower(commit.SHA)) {
		return "", errors.New("GitHub returned an invalid main commit")
	}
	return strings.ToLower(commit.SHA), nil
}

func fetchMainCommits() ([]remoteCommit, error) {
	if err := automation.RequireOnline("main commit discovery"); err != nil {
		return nil, err
	}
	var commits []remoteCommit
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/wago-org/wago/commits?sha=main&per_page=100&page=%d", releaseAPI(), page)
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub returned %s", resp.Status)
		}
		var batch []remoteCommit
		decodeErr := json.NewDecoder(resp.Body).Decode(&batch)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		commits = append(commits, batch...)
		if len(batch) < 100 {
			return commits, nil
		}
	}
}

func latestChannelRelease(channel string) (string, error) {
	if err := automation.RequireOnline("release channel discovery"); err != nil {
		return "", err
	}
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
