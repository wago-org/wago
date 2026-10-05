//go:build linux

package build

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

const nonOwnerNamespaceMarker = "WAGO_NONOWNER_NAMESPACE_READY"

func TestBuildRejectsNonOwnerUnixOutput(t *testing.T) {
	switch os.Getenv("WAGO_BUILD_NONOWNER_STAGE") {
	case "namespace":
		fmt.Fprintln(os.Stderr, nonOwnerNamespaceMarker)
		runNonOwnerBuildCases(t)
		return
	case "probe":
		// This is the operation used by the former os.WriteFile publication path.
		// Writing the same bytes proves the non-owner is authorized without changing
		// the artifact that the rejection assertions inspect afterward.
		if err := os.WriteFile(os.Getenv("WAGO_BUILD_OUTPUT"), []byte("existing artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	case "build":
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}

	if os.Geteuid() == 0 {
		runNonOwnerBuildCases(t)
		return
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skipf("user namespaces unavailable: %v", err)
	}
	child := exec.Command(unshare, "--map-auto", "--map-root-user",
		os.Args[0], "-test.run=^TestBuildRejectsNonOwnerUnixOutput$", "-test.v")
	child.Env = append(os.Environ(), "WAGO_BUILD_NONOWNER_STAGE=namespace")
	combined, err := child.CombinedOutput()
	if err != nil {
		if !bytes.Contains(combined, []byte(nonOwnerNamespaceMarker)) {
			t.Skipf("mapped user namespace unavailable: %v: %s", err, combined)
		}
		t.Fatalf("non-owner namespace test failed: %v\n%s", err, combined)
	}
}

func runNonOwnerBuildCases(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatalf("non-owner test setup requires namespace root, got euid %d", os.Geteuid())
	}
	fixtureRoot, err := os.MkdirTemp(os.TempDir(), "wago-nonowner-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureRoot) })
	if err := os.Chmod(fixtureRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(fixtureRoot, "build-test-helper")
	copyNonOwnerTestExecutable(t, helper)
	for _, throughSymlink := range []bool{false, true} {
		name := "direct"
		if throughSymlink {
			name = "symlink-target"
		}
		t.Run(name, func(t *testing.T) {
			// Use the system temporary directory directly: t.TempDir's test-owned
			// ancestors may be mode 0700 and hide the fixture from the writer UID.
			dir, err := os.MkdirTemp(fixtureRoot, "case-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			if err := os.Chmod(dir, 0o777); err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(dir, "input.wasm")
			if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o644); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "artifact.wago")
			original := []byte("existing artifact")
			if err := os.WriteFile(target, original, 0o660); err != nil {
				t.Fatal(err)
			}
			const ownerUID, writerID = 1, 2
			if err := os.Chown(target, ownerUID, writerID); err != nil {
				t.Skipf("mapped ownership unavailable: %v", err)
			}
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				requireTestSymlink(t, output)
			}

			credential := &syscall.SysProcAttr{Credential: &syscall.Credential{
				Uid: writerID, Gid: writerID,
			}}
			probe := exec.Command(helper, "-test.run=^TestBuildRejectsNonOwnerUnixOutput$")
			probe.Env = append(os.Environ(),
				"WAGO_BUILD_NONOWNER_STAGE=probe", "WAGO_BUILD_OUTPUT="+output,
			)
			probe.SysProcAttr = credential
			if combined, err := probe.CombinedOutput(); err != nil {
				t.Fatalf("old in-place write authorization probe failed: %v\n%s", err, combined)
			}

			child := exec.Command(helper, "-test.run=^TestBuildRejectsNonOwnerUnixOutput$")
			child.Env = append(os.Environ(),
				"WAGO_BUILD_NONOWNER_STAGE=build",
				"WAGO_BUILD_INPUT="+input,
				"WAGO_BUILD_OUTPUT="+output,
				"WAGO_CONFIG="+filepath.Join(dir, "settings.json"),
			)
			child.SysProcAttr = credential
			combined, err := child.CombinedOutput()
			if err == nil || !strings.Contains(string(combined), "atomic replacement requires permission to preserve existing ownership") {
				t.Fatalf("non-owner output error = %v: %s", err, combined)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("rejected build changed artifact to %q", got)
			}
			info, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if stat.Uid != ownerUID || stat.Gid != writerID || info.Mode().Perm() != 0o660 {
				t.Fatalf("rejected artifact metadata = uid %d gid %d mode %03o", stat.Uid, stat.Gid, info.Mode().Perm())
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".wago-atomic-") {
					t.Fatalf("ownership rejection left staging file %s", entry.Name())
				}
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}

func copyNonOwnerTestExecutable(t *testing.T, destination string) {
	t.Helper()
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, copyErr := io.Copy(target, source); copyErr != nil {
		_ = target.Close()
		t.Fatal(copyErr)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
}
