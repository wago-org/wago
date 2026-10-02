//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package semanticcorpus

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestSemanticCorpusManifest is the provenance/inventory gate: every manifest
// row must reference a checked-in artifact whose bytes match the pinned digest,
// and the manifest itself must be strictly valid. No toolchain is required.
func TestSemanticCorpusManifest(t *testing.T) {
	m := loadManifest(t)
	for _, mod := range m.Modules {
		path := filepath.Join(CorpusRoot(), filepath.FromSlash(mod.Artifact))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: read artifact: %v", mod.ID, err)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != mod.ArtifactSHA256 {
			t.Errorf("%s: artifact SHA-256 = %s, want %s", mod.ID, got, mod.ArtifactSHA256)
		}
		licenseDir := filepath.Join(CorpusRoot(), filepath.Dir(filepath.FromSlash(mod.Artifact)))
		if _, err := os.Stat(filepath.Join(licenseDir, "LICENSE")); err != nil {
			t.Errorf("%s: copied upstream license: %v", mod.ID, err)
		}
	}
}

// TestSemanticCorpus executes every manifest case through the wago core API and
// checks the exact oracle. Each case runs a fresh instance and a second instance
// from the same compiled module; both must reproduce the identical result.
func TestSemanticCorpus(t *testing.T) {
	m := loadManifest(t)
	for _, mod := range m.Modules {
		mod := mod
		t.Run(mod.ID, func(t *testing.T) {
			if mod.KnownIssue != "" {
				t.Skipf("known issue: %s", mod.KnownIssue)
			}
			if err := Run(CorpusRoot(), mod); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func loadManifest(t *testing.T) *Manifest {
	t.Helper()
	m, err := LoadManifest(ManifestPath())
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.Modules) == 0 {
		t.Fatalf("manifest is empty")
	}
	return m
}
