//go:build linux

package build

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/unix"
)

const stickyCapabilityNamespaceMarker = "WAGO_STICKY_CAPABILITY_NAMESPACE_READY"

func TestBuildRejectsChownedStageInStickyDirectory(t *testing.T) {
	switch os.Getenv("WAGO_BUILD_STICKY_CAP_STAGE") {
	case "namespace":
		fmt.Fprintln(os.Stderr, stickyCapabilityNamespaceMarker)
		runStickyCapabilityBuildCase(t)
		return
	case "build":
		// Linux capabilities are per-thread. Pin this helper so the race runtime
		// cannot migrate the build onto a thread that still has CAP_FOWNER.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		dropLinuxTestCapabilitiesToChown(t)
		// The former in-place publication can write this world-writable target.
		// Rewriting identical bytes proves authorization without changing assertions.
		if err := os.WriteFile(os.Getenv("WAGO_BUILD_OUTPUT"), []byte("existing artifact"), 0o666); err != nil {
			t.Fatalf("old in-place write authorization probe: %v", err)
		}
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	if os.Geteuid() == 0 {
		runStickyCapabilityBuildCase(t)
		return
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skipf("user namespaces unavailable: %v", err)
	}
	child := exec.Command(unshare, "--map-auto", "--map-root-user",
		os.Args[0], "-test.run=^TestBuildRejectsChownedStageInStickyDirectory$", "-test.v")
	child.Env = append(os.Environ(), "WAGO_BUILD_STICKY_CAP_STAGE=namespace")
	combined, err := child.CombinedOutput()
	if err != nil {
		if !bytes.Contains(combined, []byte(stickyCapabilityNamespaceMarker)) {
			t.Skipf("mapped user namespace unavailable: %v: %s", err, combined)
		}
		t.Fatalf("sticky capability namespace test failed: %v\n%s", err, combined)
	}
}

func runStickyCapabilityBuildCase(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatalf("sticky capability setup requires namespace root, got euid %d", os.Geteuid())
	}
	dir, err := os.MkdirTemp(os.TempDir(), "wago-sticky-output-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	helper := filepath.Join(dir, "build-test-helper")
	copyNonOwnerTestExecutable(t, helper)
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "artifact.wago")
	original := []byte("existing artifact")
	if err := os.WriteFile(target, original, 0o666); err != nil {
		t.Fatal(err)
	}
	const establishedOwner = 1
	if err := os.Chown(target, establishedOwner, establishedOwner); err != nil {
		t.Skipf("mapped target ownership unavailable: %v", err)
	}
	if err := os.Chmod(target, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(dir, establishedOwner, establishedOwner); err != nil {
		t.Skipf("mapped directory ownership unavailable: %v", err)
	}
	if err := os.Chmod(dir, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}

	child := exec.Command(helper, "-test.run=^TestBuildRejectsChownedStageInStickyDirectory$")
	child.Env = append(os.Environ(),
		"WAGO_BUILD_STICKY_CAP_STAGE=build", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+target,
		"WAGO_CONFIG="+filepath.Join(dir, "settings.json"),
	)
	combined, err := child.CombinedOutput()
	// The atomic writer's authoritative chown/chmod sequence may reject this
	// capability combination late instead of duplicating capability preflights in
	// the command. Either way, publication must fail before rename and clean its
	// private stage without touching the established artifact.
	if err == nil || !strings.Contains(string(combined), "set temporary file mode") {
		t.Fatalf("sticky-directory output error = %v: %s", err, combined)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("rejected artifact = %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wago-atomic-") {
			t.Fatalf("sticky-directory rejection left staging file %s", entry.Name())
		}
	}
}

func dropLinuxTestCapabilitiesToChown(t *testing.T) {
	t.Helper()
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	data[0].Effective = uint32(1) << unix.CAP_CHOWN
	data[0].Permitted = uint32(1) << unix.CAP_CHOWN
	if err := unix.Capset(&header, &data[0]); err != nil {
		t.Skipf("setting a CAP_CHOWN-only fixture is unavailable: %v", err)
	}
}
