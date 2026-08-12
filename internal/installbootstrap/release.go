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

// Resolve selects the release tag named by version. main means the newest
// canary, nightly means the newest nightly, latest follows the latest release,
// and explicit release tags pass through without a catalog request.
func Resolve(version string, catalog Catalog) (string, error) {
	resolved, err := ResolveRelease(version, catalog)
	return resolved.Tag, err
}

// ResolveRelease selects the exact release and source identity named by
// version. Once discovery knows a full commit SHA or stable tag, every fallback
// uses that immutable reference rather than the original mutable channel.
func ResolveRelease(version string, catalog Catalog) (ResolvedRelease, error) {
	version = strings.TrimSpace(version)
	switch {
	case version == "latest":
		item, err := catalog.Latest()
		if err != nil {
			return ResolvedRelease{}, err
		}
		return resolvedRelease(item)
	case IsReleaseTag(version):
		return ResolvedRelease{Tag: version, SourceRef: version}, nil
	}
	if channel, sha, canonical := rollingCommit(version); canonical {
		releases, err := catalog.Releases()
		if err != nil {
			return ResolvedRelease{}, err
		}
		sort.SliceStable(releases, func(a, b int) bool { return releases[a].PublishedAt > releases[b].PublishedAt })
		for _, item := range releases {
			if !item.Draft && strings.HasPrefix(item.TagName, channel+"-") && strings.EqualFold(item.TargetCommitish, sha) {
				return ResolvedRelease{Tag: item.TagName, SourceRef: strings.ToLower(sha)}, nil
			}
		}
		return ResolvedRelease{}, fmt.Errorf("no %s installer release found for commit %s", channel, sha)
	}
	channel := version
	if version == "main" {
		channel = "canary"
	}
	if channel != "canary" && channel != "nightly" {
		return ResolvedRelease{}, errors.New("custom source ref requires a source build")
	}
	releases, err := catalog.Releases()
	if err != nil {
		return ResolvedRelease{}, err
	}
	sort.SliceStable(releases, func(a, b int) bool { return releases[a].PublishedAt > releases[b].PublishedAt })
	for _, item := range releases {
		if !item.Draft && strings.HasPrefix(item.TagName, channel+"-") {
			return resolvedRelease(item)
		}
	}
	return ResolvedRelease{}, fmt.Errorf("no %s installer release found", channel)
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
	return version != "" && !strings.Contains(version, "@") &&
		(strings.HasPrefix(version, "v") || strings.HasPrefix(version, "canary-") || strings.HasPrefix(version, "nightly-"))
}

func rollingCommit(version string) (channel, sha string, ok bool) {
	channel, sha, found := strings.Cut(strings.ToLower(strings.TrimSpace(version)), "@")
	if !found || (channel != "canary" && channel != "nightly") || len(sha) != 40 {
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
