package version

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wago-org/wago/cli/manager/internal/tui"
	"github.com/wago-org/wago/internal/wagopaths"
)

type remoteRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
}

type remoteCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Date string `json:"date"`
		} `json:"author"`
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// isRollingChannel reports whether ver names a rolling release channel rather
// than a pinned, immutable version.
func isRollingChannel(ver string) bool { return rollingChannels[ver] }

// channelRelease returns the moving channel represented by an immutable
// prerelease tag, if any. Release APIs return newest-first, so callers keep the
// first tag seen for each channel.
func channelRelease(tag string) string {
	for channel := range rollingChannels {
		if strings.HasPrefix(tag, channel+"-") {
			return channel
		}
	}
	return ""
}

func channelReleaseNames(tags []string, channel string) []string {
	names := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if channelRelease(tag) == channel && !seen[tag] {
			names = append(names, tag)
			seen[tag] = true
		}
	}
	return names
}

func stableReleaseNames(tags []string) []string {
	names := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if tag == "" || isRollingChannel(tag) || channelRelease(tag) != "" {
			continue
		}
		name := tag
		if !strings.HasPrefix(name, "v") {
			name = "v" + name
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	sort.Slice(names, func(i, j int) bool { return Compare(names[i], names[j]) > 0 })
	return names
}

func releasePickerItems(releases []remoteRelease, channel string, now time.Time) []tui.Item {
	items := make([]tui.Item, 0, len(releases))
	for _, release := range releases {
		if channel == "" {
			if release.TagName == "" || isRollingChannel(release.TagName) || channelRelease(release.TagName) != "" {
				continue
			}
		} else if channelRelease(release.TagName) != channel {
			continue
		}
		label := releasePickerLabel(release.TagName)
		items = append(items, tui.Item{
			Label:       label,
			Description: releasePickerDescription(release, now),
			Value:       release.TagName,
		})
	}
	if channel == "" {
		sort.Slice(items, func(i, j int) bool { return Compare(items[i].Label, items[j].Label) > 0 })
	}
	return items
}

func canaryCommitItems(commits []remoteCommit, now time.Time) []tui.Item {
	items := make([]tui.Item, 0, len(commits))
	seen := make(map[string]bool, len(commits))
	for _, commit := range commits {
		sha := strings.ToLower(strings.TrimSpace(commit.SHA))
		if !validCommitSHA(sha) || seen[sha] {
			continue
		}
		seen[sha] = true
		published := commit.Commit.Author.Date
		if published == "" {
			published = commit.Commit.Committer.Date
		}
		items = append(items, tui.Item{
			Label:       "canary-" + sha[:7],
			Description: releasePickerDescription(remoteRelease{PublishedAt: published}, now),
			Value:       canaryCommitTarget(sha),
		})
	}
	return items
}

func canaryCommitTarget(sha string) string {
	return "canary@" + strings.ToLower(strings.TrimSpace(sha))
}

func canaryCommitSHA(target string) (string, bool) {
	sha, found := strings.CutPrefix(target, "canary@")
	return sha, found && validCommitSHA(sha)
}

func validCommitSHA(sha string) bool {
	if len(sha) != 40 {
		return false
	}
	for _, char := range sha {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func canaryCommitVersion(target string) string {
	if sha, ok := canaryCommitSHA(target); ok {
		return "canary-" + sha[:7]
	}
	return target
}

func releasePickerLabel(tag string) string {
	if isRollingChannel(tag) || tag == "latest" {
		return tag
	}
	if channelRelease(tag) != "" {
		parts := strings.Split(tag, "-")
		if len(parts) >= 3 && len(parts[1]) == 8 {
			if _, ok := ParseNumeric(parts[1]); ok {
				return parts[0] + "-" + strings.Join(parts[2:], "-")
			}
		}
		return tag
	}
	if strings.HasPrefix(tag, "v") {
		return tag
	}
	return "v" + tag
}

func releasePickerDescription(release remoteRelease, now time.Time) string {
	published, err := time.Parse(time.RFC3339, release.PublishedAt)
	if err != nil {
		return ""
	}
	return published.UTC().Format("01/02/2006") + "  " + relativeReleaseAge(published, now)
}

func relativeReleaseAge(published, now time.Time) string {
	elapsed := now.Sub(published)
	future := elapsed < 0
	if future {
		elapsed = -elapsed
	}
	value, unit := int(elapsed.Minutes()), "m"
	switch {
	case elapsed < time.Hour:
	case elapsed < 24*time.Hour:
		value, unit = int(elapsed.Hours()), "h"
	case elapsed < 365*24*time.Hour:
		value, unit = int(elapsed.Hours()/24), "d"
	default:
		value, unit = int(elapsed.Hours()/(365*24)), "y"
	}
	if future {
		return fmt.Sprintf("in %d%s", value, unit)
	}
	return fmt.Sprintf("%d%s ago", value, unit)
}

func releasePickerChildren(channel string, versions []tui.Item) []tui.Item {
	if len(versions) == 0 {
		return nil
	}
	items := []tui.Item{{Label: "latest", Value: channel}}
	return append(items, versions...)
}

func versionPickerItemsWithCommits(releases []remoteRelease, commits []remoteCommit, now time.Time) []tui.Item {
	canary := canaryCommitItems(commits, now)
	nightly := releasePickerItems(releases, "nightly", now)
	stable := releasePickerItems(releases, "", now)
	items := []tui.Item{
		{Label: "canary", Value: "canary", Children: releasePickerChildren("canary", canary)},
		{Label: "nightly", Value: "nightly", Children: releasePickerChildren("nightly", nightly)},
		{Label: "latest", Value: "latest", Children: releasePickerChildren("latest", stable)},
	}
	return append(items, stable...)
}

func installPickerItemsWithCommits(releases []remoteRelease, commits []remoteCommit, now time.Time) []tui.Item {
	return addProfileChoices(versionPickerItemsWithCommits(releases, commits, now))
}

func addProfileChoices(items []tui.Item) []tui.Item {
	for i := range items {
		items[i].Children = addProfileChoices(items[i].Children)
		if items[i].Value == "" {
			continue
		}
		items[i].AcceptTitle = "Choose Wago profile"
		items[i].AcceptItems = make([]tui.Item, 0, len(wagopaths.Profiles))
		for _, profile := range wagopaths.Profiles {
			builds := make([]tui.Item, 0, len(wagopaths.Builds))
			for _, build := range wagopaths.Builds {
				builds = append(builds, tui.Item{
					Label:       titleBuild(build),
					Description: build.Description(),
					Value:       installedSelectionValue(items[i].Value, profile, build),
				})
			}
			items[i].AcceptItems = append(items[i].AcceptItems, tui.Item{
				Label:       titleProfile(profile),
				Description: profile.Description(),
				AcceptTitle: "Choose Wago build",
				AcceptItems: builds,
			})
		}
	}
	return items
}

func chooseInstallPicker(releases []remoteRelease, commits []remoteCommit, now time.Time, profileValue, buildValue string) (string, wagopaths.Profile, wagopaths.Build, bool) {
	if profileValue != "" || buildValue != "" {
		choice, ok := tui.Choose("Install Wago version", versionPickerItemsWithCommits(releases, commits, now))
		if !ok {
			return "", "", "", false
		}
		profile, build, ok := chooseInstallVariant(profileValue, buildValue)
		return choice, profile, build, ok
	}
	p := tui.NewPicker("Install Wago version", installPickerItemsWithCommits(releases, commits, now))
	submitted, cancelled := tui.Run(p)
	if !submitted || cancelled {
		return "", "", "", false
	}
	return parseInstalledSelection(p.Selected())
}

// updateVersionTarget chooses the version refreshed by `wago version update`.
// No selector means the active version; explicit channel flags make refreshing a
// rolling channel convenient without first selecting it.
func updateVersionTarget(active string, args []string, nightly, canary bool) (string, error) {
	if nightly && canary {
		return "", fmt.Errorf("--nightly and --canary cannot be used together")
	}
	if len(args) > 1 {
		return "", fmt.Errorf("accepts at most one [version]")
	}
	if (nightly || canary) && len(args) != 0 {
		return "", fmt.Errorf("a release-channel flag cannot be used with [version]")
	}
	if nightly {
		return "nightly", nil
	}
	if canary {
		return "canary", nil
	}
	if len(args) == 1 {
		if !isRollingChannel(args[0]) {
			return "", fmt.Errorf("%s is pinned; update only refreshes canary or nightly", args[0])
		}
		return args[0], nil
	}
	if active == "" {
		return "", fmt.Errorf("no active version; use `wago version update <version>` or select --nightly/--canary")
	}
	if !isRollingChannel(active) {
		return "", fmt.Errorf("active version %s is pinned; switch to canary or nightly to update", active)
	}
	return active, nil
}
