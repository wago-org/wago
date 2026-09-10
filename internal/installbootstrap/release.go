// Package installbootstrap owns Runtime Installation release selection and
// checksum verification. Platform bootstraps remain native adapters; the
// downloaded installer and its tests use this module as the policy surface.
package installbootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type Release struct {
	TagName         string `json:"tag_name"`
	TargetCommitish string `json:"target_commitish"`
	PublishedAt     string `json:"published_at"`
	Draft           bool   `json:"draft"`
}

// ResolvedRelease preserves the immutable identity selected during discovery.
// Tag is the exact release asset namespace; SourceRef is the exact Git/archive
// reference and never degrades to a mutable channel after resolution.
type ResolvedRelease struct {
	Tag       string
	SourceRef string
}

// Catalog is the release-discovery seam. GitHub HTTP and in-memory tests are
// the two adapters; release selection itself remains local and deterministic.
type Catalog interface {
	Latest() (Release, error)
	Releases() ([]Release, error)
}

// Resolve selects the preferred release tag named by version. main prefers the
// newest official release, then beta, then canary. Explicit channels, latest,
// and release tags select only the requested release.
func Resolve(version string, catalog Catalog) (string, error) {
	resolved, err := ResolveRelease(version, catalog)
	return resolved.Tag, err
}

// ResolveRelease selects the exact release and source identity named by
// version. Once discovery knows a full commit SHA or stable tag, every fallback
// uses that immutable reference rather than the original mutable channel.
func ResolveRelease(version string, catalog Catalog) (ResolvedRelease, error) {
	candidates, err := ResolveReleaseCandidates(version, catalog)
	if err != nil {
		return ResolvedRelease{}, err
	}
	return candidates[0], nil
}

// ResolveReleaseCandidates returns manager releases in download preference
// order. Only main has cross-channel fallback; every explicit selector remains
// exact so a caller never silently installs a different requested channel.
func ResolveReleaseCandidates(version string, catalog Catalog) ([]ResolvedRelease, error) {
	version = strings.TrimSpace(version)
	switch {
	case version == "latest":
		item, err := catalog.Latest()
		if err != nil {
			return nil, err
		}
		resolved, err := resolvedRelease(item)
		return []ResolvedRelease{resolved}, err
	case IsReleaseTag(version):
		return []ResolvedRelease{{Tag: version, SourceRef: version}}, nil
	}
	if channel, sha, canonical := rollingCommit(version); canonical {
		releases, err := catalog.Releases()
		if err != nil {
			return nil, err
		}
		sort.SliceStable(releases, func(a, b int) bool { return releases[a].PublishedAt > releases[b].PublishedAt })
		for _, item := range releases {
			if !item.Draft && releaseChannel(item.TagName) == channel && strings.EqualFold(item.TargetCommitish, sha) {
				return []ResolvedRelease{{Tag: item.TagName, SourceRef: strings.ToLower(sha)}}, nil
			}
		}
		return nil, fmt.Errorf("no %s installer release found for commit %s", channel, sha)
	}
	if version != "main" && version != "canary" && version != "beta" {
		return nil, errors.New("custom source ref requires a source build")
	}
	candidates := make([]ResolvedRelease, 0, 3)
	if version == "main" {
		latest, latestErr := catalog.Latest()
		if latestErr == nil && !latest.Draft && releaseChannel(latest.TagName) == "official" {
			resolved, err := resolvedRelease(latest)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, resolved)
		}
	}
	releases, err := catalog.Releases()
	if err != nil {
		if len(candidates) != 0 {
			return candidates, nil
		}
		return nil, err
	}
	sort.SliceStable(releases, func(a, b int) bool { return releases[a].PublishedAt > releases[b].PublishedAt })
	channels := []string{version}
	if version == "main" {
		channels = []string{"beta", "canary"}
	}
	seen := make(map[string]bool, len(channels))
	for _, item := range releases {
		channel := releaseChannel(item.TagName)
		if item.Draft || seen[channel] {
			continue
		}
		for _, wanted := range channels {
			if channel == wanted {
				resolved, err := resolvedRelease(item)
				if err != nil {
					return nil, err
				}
				candidates = append(candidates, resolved)
				seen[channel] = true
				break
			}
		}
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		return channelPriority(releaseChannel(candidates[a].Tag)) < channelPriority(releaseChannel(candidates[b].Tag))
	})
	if len(candidates) == 0 {
		if version == "main" {
			return nil, errors.New("no official, beta, or canary installer release found")
		}
		return nil, fmt.Errorf("no %s installer release found", version)
	}
	return candidates, nil
}

func channelPriority(channel string) int {
	switch channel {
	case "official":
		return 0
	case "beta":
		return 1
	default:
		return 2
	}
}

func resolvedRelease(item Release) (ResolvedRelease, error) {
	if item.TagName == "" {
		return ResolvedRelease{}, errors.New("release response did not contain a tag")
	}
	ref := strings.ToLower(strings.TrimSpace(item.TargetCommitish))
	if !fullCommitSHA(ref) {
		ref = item.TagName
	}
	return ResolvedRelease{Tag: item.TagName, SourceRef: ref}, nil
}

func fullCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func IsReleaseTag(version string) bool {
	version = strings.TrimSpace(version)
	return version != "" && !strings.Contains(version, "@") && strings.HasPrefix(version, "v")
}

func releaseChannel(tag string) string {
	version, prerelease, found := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
	if !validReleaseCore(version) {
		return ""
	}
	if !found {
		return "official"
	}
	channel, identity, found := strings.Cut(prerelease, ".")
	if !found {
		return ""
	}
	switch channel {
	case "beta":
		if !canonicalNumber(identity) {
			return ""
		}
	case "canary":
		if !strings.HasPrefix(identity, "g") || !shortCommitSHA(strings.TrimPrefix(identity, "g")) {
			return ""
		}
	default:
		return ""
	}
	return channel
}

func shortCommitSHA(value string) bool {
	if len(value) != 7 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func validReleaseCore(tag string) bool {
	if !strings.HasPrefix(tag, "v") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	return len(parts) == 3 && canonicalNumber(parts[0]) && canonicalNumber(parts[1]) && canonicalNumber(parts[2])
}

func canonicalNumber(value string) bool {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func rollingCommit(version string) (channel, sha string, ok bool) {
	channel, sha, found := strings.Cut(strings.ToLower(strings.TrimSpace(version)), "@")
	if !found || (channel != "canary" && channel != "beta") || len(sha) != 40 {
		return "", "", false
	}
	for _, char := range sha {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return "", "", false
		}
	}
	return channel, sha, true
}

func Asset(prefix, goos, goarch string) (string, error) {
	if goos != "linux" && goos != "darwin" && goos != "windows" {
		return "", fmt.Errorf("unsupported operating system %q", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("unsupported architecture %q", goarch)
	}
	return prefix + "-" + goos + "-" + goarch, nil
}

// VerifyFile validates the first SHA-256 field in checksumData against path.
func VerifyFile(path string, checksumData []byte) error {
	fields := strings.Fields(string(checksumData))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return errors.New("release checksum is malformed")
	}
	want, err := hex.DecodeString(fields[0])
	if err != nil {
		return errors.New("release checksum is malformed")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), hex.EncodeToString(want)) {
		return errors.New("release checksum did not match")
	}
	return nil
}
