//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
)

func TestWazeroPortPinnedCoreV2SpecExecution(t *testing.T) {
	root := filepath.Clean("../../tests/spec-v2")
	if _, err := os.Stat(filepath.Join(root, "test", "core")); err != nil {
		if os.IsNotExist(err) {
			t.Skip("pinned Core v2 submodule is not initialized; run make spec2 for mandatory execution")
		}
		t.Fatal(err)
	}
	suite, err := spectest.DiscoverRelease2(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.Files) != 147 {
		t.Fatalf("pinned Core v2 manifest has %d WAST files, want 147", len(suite.Files))
	}
	digest, err := spectest.Release2Digest(suite)
	if err != nil {
		t.Fatal(err)
	}
	if want := "124235f8e65d0c4f5454e369e6c8d9695a1f0264ca45d3e634614884a491fb0a"; digest != want {
		t.Fatalf("pinned Core v2 digest = %s, want %s", digest, want)
	}
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skipf("wast2json is unavailable in the ordinary test environment; run make spec2 for mandatory execution: %v", err)
	}
	t.Setenv("WAGO_SPECTEST_DIR", root)
	t.Setenv("WAGO_SPEC_VERSION", "2.0")
	TestSpecSuiteExec(t)
}
