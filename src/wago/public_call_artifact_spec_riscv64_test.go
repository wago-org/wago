//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/wago-org/wago/tests/support/simdfixture"
)

const rv64CallSpecMaxArtifactBytes = 16 << 20

type rv64CallSpecArtifactBinding struct {
	SourceSHA256   string `json:"source_sha256"`
	FixturesSHA256 string `json:"fixtures_sha256"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}

func rv64PublicCallSpecArtifact(t *testing.T, mode, dir, profile string, source []byte, fixturesHash string, policy rv64PublicSpecPolicy) []byte {
	t.Helper()
	sourceHash := sha256.Sum256(source)
	binding := rv64CallSpecArtifactBinding{SourceSHA256: hex.EncodeToString(sourceHash[:]), FixturesSHA256: fixturesHash}

	metadata, err := simdfixture.Read(dir, profile+".wago.json", 4096)
	if err != nil {
		t.Fatal(err)
	}
	var recorded rv64CallSpecArtifactBinding
	if err := simdfixture.Decode(metadata, &recorded); err != nil {
		t.Fatal(err)
	}
	blob, err := simdfixture.Read(dir, profile+".wago", rv64CallSpecMaxArtifactBytes)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(blob)
	binding.ArtifactSHA256 = hex.EncodeToString(hash[:])
	if recorded != binding {
		t.Fatal("prepared call artifact source/fixture/digest binding differs")
	}
	return blob
}

func rv64PublicCallSpecWriteExclusive(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	n, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil || n != len(data) {
		t.Fatal("write prepared call artifact", writeErr, closeErr, n)
	}
}
