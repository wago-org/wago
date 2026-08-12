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
		latest: Release{TagName: "v1.2.3"},
		releases: []Release{
			{TagName: "canary-draft", TargetCommitish: canarySHA, PublishedAt: "2026-08-05T00:00:00Z", Draft: true},
			{TagName: "canary-old", TargetCommitish: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PublishedAt: "2026-08-01T00:00:00Z"},
			{TagName: "nightly-new", TargetCommitish: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PublishedAt: "2026-08-04T00:00:00Z"},
			{TagName: "canary-new", TargetCommitish: canarySHA, PublishedAt: "2026-08-03T00:00:00Z"},
		},
	}
	for _, test := range []struct{ version, want string }{
		{"latest", "v1.2.3"}, {"main", "canary-new"}, {"canary", "canary-new"},
		{"canary@" + canarySHA, "canary-new"}, {"nightly", "nightly-new"},
		{"v9.0.0", "v9.0.0"}, {"canary-pinned", "canary-pinned"},
	} {
		got, err := Resolve(test.version, catalog)
		if err != nil || got != test.want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", test.version, got, err, test.want)
		}
	}
	if _, err := Resolve("feature/ref", catalog); err == nil {
		t.Fatal("custom source ref resolved as a release")
	}
	if _, err := Resolve("nightly@cccccccccccccccccccccccccccccccccccccccc", catalog); err == nil {
		t.Fatal("unpublished canonical commit resolved as a release")
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
