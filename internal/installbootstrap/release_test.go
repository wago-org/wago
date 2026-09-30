package installbootstrap

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type memoryCatalog struct {
	latest   Release
	releases []Release
}

func (catalog memoryCatalog) Latest() (Release, error) { return catalog.latest, nil }
func (catalog memoryCatalog) Releases() ([]Release, error) {
	return append([]Release(nil), catalog.releases...), nil
}

func TestResolveReleaseContract(t *testing.T) {
	const canarySHA = "deadbee123456789012345678901234567890123"
	catalog := memoryCatalog{
		latest: Release{TagName: "v1.2.3", TargetCommitish: "cccccccccccccccccccccccccccccccccccccccc"},
		releases: []Release{
			{TagName: "v1.2.3", TargetCommitish: "cccccccccccccccccccccccccccccccccccccccc", PublishedAt: "2026-08-02T00:00:00Z"},
			{TagName: "v0.1.0-canary.gdeadbee", TargetCommitish: canarySHA, PublishedAt: "2026-08-05T00:00:00Z", Draft: true},
			{TagName: "v0.1.0-canary.gaaaaaaa", TargetCommitish: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PublishedAt: "2026-08-01T00:00:00Z"},
			{TagName: "v0.1.0-beta.2", TargetCommitish: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PublishedAt: "2026-08-04T00:00:00Z"},
			{TagName: "v0.1.0-canary.gdeadbee", TargetCommitish: canarySHA, PublishedAt: "2026-08-03T00:00:00Z"},
		},
	}
	for _, test := range []struct{ version, want string }{
		{"latest", "v1.2.3"}, {"main", "v1.2.3"}, {"beta", "v0.1.0-beta.2"},
		{" v9.0.0 ", "v9.0.0"},
	} {
		got, err := Resolve(test.version, catalog)
		if err != nil || got != test.want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", test.version, got, err, test.want)
		}
	}
	if _, err := Resolve("feature/ref", catalog); err == nil {
		t.Fatal("custom source ref resolved as a release")
	}
	if _, err := Resolve("beta@cccccccccccccccccccccccccccccccccccccccc", catalog); err == nil {
		t.Fatal("unpublished canonical commit resolved as a release")
	}
	if _, err := Resolve("v0.1.0-beta.1@cccccccccccccccccccccccccccccccccccccccc", catalog); err == nil {
		t.Fatal("tag plus unverified commit suffix bypassed release resolution")
	}
	if IsReleaseTag("v0.1.0-canary.gdeadbee@cccccccccccccccccccccccccccccccccccccccc") {
		t.Fatal("release tag accepted an appended commit identity")
	}
	if _, err := ResolveRelease("canary", catalog); err == nil {
		t.Fatal("canary unexpectedly used the release catalog")
	}
	if _, err := ResolveRelease("canary@"+canarySHA, catalog); err == nil {
		t.Fatal("canonical canary unexpectedly used the release catalog")
	}
	resolved, err := ResolveRelease("main", catalog)
	if err != nil || resolved.Tag != "v1.2.3" || resolved.SourceRef != "cccccccccccccccccccccccccccccccccccccccc" {
		t.Fatalf("ResolveRelease channel = %+v, %v", resolved, err)
	}
	resolved, err = ResolveRelease("v9.0.0", catalog)
	if err != nil || resolved.Tag != "v9.0.0" || resolved.SourceRef != "v9.0.0" {
		t.Fatalf("ResolveRelease stable = %+v, %v", resolved, err)
	}
}

func TestResolveReleaseCandidatesPrefersOfficialThenBeta(t *testing.T) {
	catalog := memoryCatalog{latest: Release{TagName: "v1.0.0"}, releases: []Release{
		{TagName: "v2.0.0-canary.gbbbbbbb", PublishedAt: "2026-08-06T00:00:00Z"},
		{TagName: "v1.1.0-beta.2", PublishedAt: "2026-08-05T00:00:00Z"},
		{TagName: "v1.0.0", PublishedAt: "2026-08-04T00:00:00Z"},
		{TagName: "v1.2.0", PublishedAt: "2026-08-07T00:00:00Z", Draft: true},
		{TagName: "v0.9.0", PublishedAt: "2026-08-03T00:00:00Z"},
	}}
	candidates, err := ResolveReleaseCandidates("main", catalog)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v1.0.0", "v1.1.0-beta.2"}
	if len(candidates) != len(want) {
		t.Fatalf("candidates = %+v, want %v", candidates, want)
	}
	for index := range want {
		if candidates[index].Tag != want[index] {
			t.Fatalf("candidates[%d] = %q, want %q", index, candidates[index].Tag, want[index])
		}
	}
	for _, test := range []struct {
		channel string
		want    string
	}{{"beta", "v1.1.0-beta.2"}} {
		candidates, err := ResolveReleaseCandidates(test.channel, catalog)
		if err != nil || len(candidates) != 1 || candidates[0].Tag != test.want {
			t.Errorf("ResolveReleaseCandidates(%q) = %+v, %v; want only %q", test.channel, candidates, err, test.want)
		}
	}
}

func TestAssetAndChecksumContract(t *testing.T) {
	asset, err := Asset("wago-installer", "windows", "arm64")
	if err != nil || asset != "wago-installer-windows-arm64" {
		t.Fatalf("asset = %q, %v", asset, err)
	}
	payload := []byte("installer")
	path := filepath.Join(t.TempDir(), "installer")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Appendf(nil, "%x  installer\n", sha256.Sum256(payload))
	if err := VerifyFile(path, checksum); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(path, []byte("bad")); err == nil {
		t.Fatal("malformed checksum accepted")
	}
}
