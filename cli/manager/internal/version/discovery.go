package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/wagopaths"
)

func latestStableReleaseContext(ctx context.Context) (string, error) {
	response, err := getReleaseBytes(ctx, "release discovery", releaseAPI()+"/repos/wago-org/wago/releases/latest", releaseMetadataMaximum)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", response.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(response.Body, &release); err != nil || release.TagName == "" {
		return "", errors.New("GitHub returned an invalid latest release")
	}
	return release.TagName, nil
}

func vmBrowseContext(ctx context.Context, d wagopaths.Dirs, profileValue, buildValue, use string) {
	releases, err := fetchReleasesContext(ctx)
	if err != nil {
		fatal("version browse: unable to fetch releases: %v", err)
	}
	commits, err := fetchMainCommitsContext(ctx)
	if err != nil {
		fatal("version browse: unable to fetch main commits: %v", err)
	}
	choice, profile, build, ok := chooseInstallPicker(releases, commits, time.Now(), profileValue, buildValue)
	if !ok {
		return
	}
	if choice == "latest" {
		release, err := latestStableReleaseContext(ctx)
		if err != nil {
			fatal("version latest: %v", err)
		}
		vmInstallContext(ctx, d, release, profile, build, use)
		return
	}
	vmInstallContext(ctx, d, choice, profile, build, use)
}

func fetchReleases() ([]remoteRelease, error) {
	return fetchReleasesContext(context.Background())
}

const discoveryPageLimit = 10

func fetchReleasesContext(ctx context.Context) ([]remoteRelease, error) {
	var releases []remoteRelease
	err := forEachReleasePage(ctx, "release discovery", func(batch []remoteRelease) bool {
		releases = append(releases, batch...)
		return true
	})
	return releases, err
}

func forEachReleasePage(ctx context.Context, operation string, visit func([]remoteRelease) bool) error {
	address := fmt.Sprintf("%s/repos/wago-org/wago/releases?per_page=100&page=1", releaseAPI())
	seen := make(map[string]struct{}, discoveryPageLimit)
	for page := 1; page <= discoveryPageLimit; page++ {
		if _, duplicate := seen[address]; duplicate {
			return fmt.Errorf("GitHub release pagination loop at page %d", page)
		}
		seen[address] = struct{}{}
		response, err := getReleaseBytes(ctx, operation, address, releaseMetadataMaximum)
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GitHub returned %s", response.Status)
		}
		var batch []remoteRelease
		if err := json.Unmarshal(response.Body, &batch); err != nil {
			return err
		}
		if len(batch) > 100 {
			return fmt.Errorf("GitHub returned too many releases on page %d", page)
		}
		if !visit(batch) {
			return nil
		}
		next, found, err := releaseNextLink(response.Header, address)
		if err != nil {
			return err
		}
		if found {
			address = next
			continue
		}
		if len(batch) < 100 {
			return nil
		}
		address = fmt.Sprintf("%s/repos/wago-org/wago/releases?per_page=100&page=%d", releaseAPI(), page+1)
	}
	return fmt.Errorf("GitHub release discovery exceeded %d pages", discoveryPageLimit)
}

func releaseNextLink(header http.Header, current string) (string, bool, error) {
	for _, value := range header.Values("Link") {
		for _, entry := range strings.Split(value, ",") {
			entry = strings.TrimSpace(entry)
			left, parameters, found := strings.Cut(entry, ";")
			if !found || !strings.Contains(parameters, `rel="next"`) {
				continue
			}
			if len(left) < 3 || left[0] != '<' || left[len(left)-1] != '>' {
				return "", false, fmt.Errorf("GitHub returned a malformed release pagination link")
			}
			base, err := url.Parse(current)
			if err != nil {
				return "", false, err
			}
			reference, err := url.Parse(left[1 : len(left)-1])
			if err != nil {
				return "", false, fmt.Errorf("GitHub returned a malformed release pagination link: %w", err)
			}
			next := base.ResolveReference(reference)
			if next.Scheme != base.Scheme || next.Host != base.Host || next.Path != base.Path {
				return "", false, fmt.Errorf("GitHub returned an invalid release pagination target")
			}
			return next.String(), true, nil
		}
	}
	return "", false, nil
}

// vmUpdate resolves the moving channel before touching the installed runtime.
// Matching commits are left intact unless force is set; replacements use a
// sibling temporary file so a failed update preserves the installed binary.
var (
	resolveUpdateRunnerVersionContext = resolveRunnerVersionContext
	installUpdateRunnerPayloadContext = installRunnerPayloadContext
)

func vmUpdate(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, use string, force bool) {
	vmUpdateContext(context.Background(), d, ver, profile, build, use, force)
}

func vmUpdateContext(ctx context.Context, d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, use string, force bool) {
	dest := d.RuntimeBinary(ver, string(profile), string(build))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version update: %v", err)
	}
	progress := managerprogress.NewProgress(os.Stderr)
	progress.Title("Updating " + installedWagoLabel(ver, ver, profile, build))
	resolved, sourceOnly, err := resolveUpdateRunnerVersionContext(ctx, ver, progress)
	if err != nil {
		fatal("version update: %v", err)
	}
	if !force && installedCommitMatches(dest, resolved) {
		progress.Finish(installedWagoLabel(ver, resolved, profile, build) + " is already up to date")
		offerUseUpdated(d, ver, profile, build, use)
		return
	}
	if err := installUpdateRunnerPayloadContext(ctx, resolved, profile, build, dest, sourceOnly, progress); err != nil {
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
	lowerInstalled := strings.ToLower(strings.TrimSpace(installed))
	rollingStamp := strings.HasPrefix(lowerInstalled, "canary@") || strings.HasPrefix(lowerInstalled, "nightly@")
	if installed != "" && installed == resolved && channelRelease(installed) == "" && !isRollingChannel(installed) && !rollingStamp {
		return true
	}
	installedChannel, installedCommit, installedCanonical := rollingCommitSHA(installed)
	resolvedChannel, resolvedCommit, resolvedCanonical := rollingCommitSHA(resolved)
	// Only canonical object IDs are safe freshness identities. Legacy abbreviated
	// stamps are deliberately treated as stale: prefix equality can collide, and
	// an offline local comparison cannot prove what object an abbreviation named.
	return installedCanonical && resolvedCanonical && installedChannel == resolvedChannel && installedCommit == resolvedCommit
}

func resolveRunnerVersion(ver string, progress *managerprogress.Progress) (resolved string, sourceOnly bool, err error) {
	return resolveRunnerVersionContext(context.Background(), ver, progress)
}

func resolveRunnerVersionContext(ctx context.Context, ver string, progress *managerprogress.Progress) (resolved string, sourceOnly bool, err error) {
	if ver == "canary" {
		if progress != nil {
			progress.Begin("resolving main commit")
		}
		sha, resolveErr := latestMainCommitContext(ctx)
		if resolveErr != nil {
			if progress != nil {
				progress.Fail("could not resolve main commit")
			}
			return "", false, resolveErr
		}
		resolved = canaryCommitTarget(sha)
		if progress != nil {
			progress.Done("resolved " + releasePickerLabel(resolved))
		}
		return resolved, false, nil
	}
	if !isRollingChannel(ver) {
		return canonicalReleaseRef(ver), false, nil
	}
	if progress != nil {
		progress.Begin("resolving release")
	}
	resolved, err = latestChannelReleaseContext(ctx, ver)
	if err == nil {
		if progress != nil {
			progress.Done("resolved " + releasePickerLabel(resolved))
		}
		return resolved, false, nil
	}
	if errors.Is(err, errNoPublishedRelease) {
		sha, resolveErr := latestMainCommitContext(ctx)
		if resolveErr != nil {
			if progress != nil {
				progress.Fail("could not resolve source commit")
			}
			return "", false, resolveErr
		}
		resolved = ver + "@" + sha
		if progress != nil {
			progress.Done("no published release; using " + releasePickerLabel(resolved) + " source")
		}
		return resolved, true, nil
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
	return installRunnerPayloadContext(context.Background(), ref, profile, build, dest, sourceOnly, progress)
}

func installRunnerPayloadContext(ctx context.Context, ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, sourceOnly bool, progress *managerprogress.Progress) error {
	if !sourceOnly {
		err := downloadBinaryWithProgressContext(ctx, releaseBase(), releaseAssetVersion(ref), profile, build, dest, progress)
		if err == nil {
			return nil
		}
		if !releaseAssetUnavailable(err) {
			return err
		}
	}
	return buildRunnerSource(ctx, ref, profile, build, dest, progress)
}

func installManagerUpdate(channel, dest string, progress *managerprogress.Progress) (string, error) {
	return installManagerUpdateContext(context.Background(), channel, dest, progress)
}

func installManagerUpdateContext(ctx context.Context, channel, dest string, progress *managerprogress.Progress) (string, error) {
	resolved, sourceOnly, err := resolveManagerUpdateContext(ctx, channel, progress)
	if err != nil {
		return "", err
	}
	if err := installManagerPayloadContext(ctx, resolved, dest, sourceOnly, progress); err != nil {
		return "", err
	}
	return resolved, nil
}

func resolveManagerUpdate(channel string, progress *managerprogress.Progress) (resolved string, sourceOnly bool, err error) {
	return resolveManagerUpdateContext(context.Background(), channel, progress)
}

func resolveManagerUpdateContext(ctx context.Context, channel string, progress *managerprogress.Progress) (resolved string, sourceOnly bool, err error) {
	if channel == "latest" {
		if progress != nil {
			progress.Begin("resolving latest release")
		}
		resolved, err = latestStableReleaseContext(ctx)
		if err == nil && progress != nil {
			progress.Done("resolved " + releasePickerLabel(resolved))
		}
	} else {
		resolved, sourceOnly, err = resolveRunnerVersionContext(ctx, channel, progress)
	}
	if err != nil {
		return "", false, err
	}
	return resolved, sourceOnly, nil
}

func installManagerPayload(resolved, dest string, sourceOnly bool, progress *managerprogress.Progress) error {
	return installManagerPayloadContext(context.Background(), resolved, dest, sourceOnly, progress)
}

func installManagerPayloadContext(ctx context.Context, resolved, dest string, sourceOnly bool, progress *managerprogress.Progress) error {
	if !sourceOnly {
		err := downloadReleaseAssetWithProgressContext(
			ctx,
			releaseBase(),
			releaseAssetVersion(resolved),
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
	if err := buildManagerSource(ctx, resolved, dest, progress); err != nil {
		return err
	}
	return nil
}

func latestMainCommit() (string, error) {
	return latestMainCommitContext(context.Background())
}

func latestMainCommitContext(ctx context.Context) (string, error) {
	response, err := getReleaseBytes(ctx, "main commit discovery", releaseAPI()+"/repos/wago-org/wago/commits/main", releaseMetadataMaximum)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", response.Status)
	}
	var commit remoteCommit
	if err := json.Unmarshal(response.Body, &commit); err != nil {
		return "", err
	}
	if !validCommitSHA(strings.ToLower(commit.SHA)) {
		return "", errors.New("GitHub returned an invalid main commit")
	}
	return strings.ToLower(commit.SHA), nil
}

func fetchMainCommits() ([]remoteCommit, error) {
	return fetchMainCommitsContext(context.Background())
}

func fetchMainCommitsContext(ctx context.Context) ([]remoteCommit, error) {
	var commits []remoteCommit
	for page := 1; page <= discoveryPageLimit; page++ {
		address := fmt.Sprintf("%s/repos/wago-org/wago/commits?sha=main&per_page=100&page=%d", releaseAPI(), page)
		response, err := getReleaseBytes(ctx, "main commit discovery", address, releaseMetadataMaximum)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub returned %s", response.Status)
		}
		var batch []remoteCommit
		if err := json.Unmarshal(response.Body, &batch); err != nil {
			return nil, err
		}
		if len(batch) > 100 {
			return nil, fmt.Errorf("GitHub returned too many commits on page %d", page)
		}
		commits = append(commits, batch...)
		if len(batch) < 100 {
			return commits, nil
		}
	}
	return nil, fmt.Errorf("GitHub commit discovery exceeded %d pages", discoveryPageLimit)
}

func latestChannelRelease(channel string) (string, error) {
	return latestChannelReleaseContext(context.Background(), channel)
}

func latestChannelReleaseContext(ctx context.Context, channel string) (string, error) {
	prefix := channel + "-"
	var resolved string
	var resolveErr error
	err := forEachReleasePage(ctx, "release channel discovery", func(releases []remoteRelease) bool {
		for _, release := range releases {
			if release.Draft || !strings.HasPrefix(release.TagName, prefix) {
				continue
			}
			sha := strings.ToLower(strings.TrimSpace(release.TargetCommitish))
			if !validCommitSHA(sha) {
				resolveErr = fmt.Errorf("GitHub release %q has an invalid target commit", release.TagName)
				return false
			}
			resolved = release.TagName + "@" + sha
			return false
		}
		return true
	})
	if err != nil {
		return "", err
	}
	if resolveErr != nil {
		return "", resolveErr
	}
	if resolved == "" {
		return "", fmt.Errorf("%w: %s", errNoPublishedRelease, channel)
	}
	return resolved, nil
}

var errNoPublishedRelease = errors.New("no published release")
