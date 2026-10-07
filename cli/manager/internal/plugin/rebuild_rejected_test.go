package plugin

import (
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
)

func TestRejectedRebuildChild(t *testing.T) {
	if os.Getenv("WAGO_TEST_REJECTED_REBUILD_CHILD") != "1" {
		return
	}
	Rebuild(MaintenanceRequest{Global: true})
	t.Fatal("rebuild unexpectedly accepted invalid plugin initialization")
}

func TestRejectedRebuildPreservesActiveRuntime(t *testing.T) {
	buildDir, _ := prepareTestPluginRuntime(t)
	active, configured, err := pluginRuntimeBinary()
	if err != nil || !configured {
		t.Fatalf("initial runtime = %q, %v, %v", active, configured, err)
	}
	previous, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	previousHash := sha256.Sum256(previous)
	if output, err := exec.Command(active).CombinedOutput(); err != nil || bytes.Contains(output, []byte("REJECTED_RUNTIME")) {
		t.Fatalf("initial runtime = %v: %s", err, output)
	}

	source := filepath.Join(os.Getenv("WAGO_SRC"), "register", "register.go")
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, []byte("\nfunc init() { println(\"REJECTED_RUNTIME\") }\n")...)
	if err := os.WriteFile(source, contents, 0o644); err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run=^TestRejectedRebuildChild$")
	child.Env = append(os.Environ(), "WAGO_TEST_REJECTED_REBUILD_CHILD=1")
	output, err := child.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "produced unexpected output") {
		t.Fatalf("rebuild = %v: %s", err, output)
	}
	activeAfter, err := os.ReadFile(pluginbuild.BinaryPath(buildDir))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(activeAfter) != previousHash {
		t.Fatal("failed rebuild replaced the verified runtime")
	}
	// A later cache miss may reject the changed source, but it must never hand
	// out the binary that the rebuild rejected.
	selected, configured, err := pluginRuntimeBinary()
	if err == nil && configured {
		selectedBytes, readErr := os.ReadFile(selected)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if sha256.Sum256(selectedBytes) != previousHash {
			t.Fatal("runtime selection accepted the rejected executable")
		}
	}
}
