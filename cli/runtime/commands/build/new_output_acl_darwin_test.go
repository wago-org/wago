//go:build darwin

package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

func TestBuildNewDarwinOutputInheritsParentACL(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read,file_inherit", dir).CombinedOutput(); err != nil {
		t.Skipf("Darwin inheritable ACL unavailable: %v: %s", err, output)
	}
	control := filepath.Join(dir, "direct-control")
	controlFile, err := os.OpenFile(control, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlFile.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.wago")
	Command(testEnvironment{}).Run(command.NewContext(
		[]string{input}, map[string]string{"output": output}, nil,
	))
	if acl := readDarwinTestAccessACL(t, output); !strings.Contains(acl, "everyone") {
		t.Fatalf("new output did not inherit parent ACL: %q", acl)
	}
	controlInfo, controlErr := os.Stat(control)
	outputInfo, outputErr := os.Stat(output)
	if controlErr != nil || outputErr != nil {
		t.Fatalf("inspect direct/private metadata: control=%v output=%v", controlErr, outputErr)
	}
	controlStat, controlOK := controlInfo.Sys().(*syscall.Stat_t)
	outputStat, outputOK := outputInfo.Sys().(*syscall.Stat_t)
	if !controlOK || !outputOK ||
		controlStat.Uid != outputStat.Uid || controlStat.Gid != outputStat.Gid ||
		controlInfo.Mode().Perm() != outputInfo.Mode().Perm() ||
		readDarwinTestAccessACL(t, control) != readDarwinTestAccessACL(t, output) {
		t.Fatalf("private publication metadata differs from direct creation: control=%v output=%v", controlInfo, outputInfo)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wago-") {
			t.Fatalf("temporary publication entry remains: %s", entry.Name())
		}
	}
}

func TestBuildNewDarwinOutputMetadataProbeNeedsNoReadAccess(t *testing.T) {
	const childEnv = "WAGO_DARWIN_WRITE_ONLY_PARENT_CHILD"
	if os.Getenv(childEnv) == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_DARWIN_WRITE_ONLY_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_DARWIN_WRITE_ONLY_OUTPUT")}, nil,
		))
		return
	}
	dir := t.TempDir()
	if output, err := exec.Command("/bin/chmod", "+a", "everyone deny read,file_inherit", dir).CombinedOutput(); err != nil {
		t.Skipf("Darwin deny-read ACL unavailable: %v: %s", err, output)
	}
	// Remove the deny ACE before t.TempDir's later cleanup, which necessarily
	// enumerates the directory and should not obscure the build assertion.
	t.Cleanup(func() { _ = exec.Command("/bin/chmod", "-N", dir).Run() })
	control := filepath.Join(dir, "write-only-control")
	file, err := os.OpenFile(control, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Skipf("filesystem cannot express write-only creation for this identity: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	inputDir := t.TempDir()
	input := filepath.Join(inputDir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.wago")
	child := exec.Command(os.Args[0], "-test.run=^TestBuildNewDarwinOutputMetadataProbeNeedsNoReadAccess$")
	child.Env = append(os.Environ(), childEnv+"=1", "WAGO_DARWIN_WRITE_ONLY_INPUT="+input,
		"WAGO_DARWIN_WRITE_ONLY_OUTPUT="+output)
	if combined, err := child.CombinedOutput(); err != nil {
		t.Fatalf("write-only-parent build failed: %v: %s", err, combined)
	}
	if info, err := os.Stat(output); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("write-authorized output = %v, %v", info, err)
	}
}

func TestBuildDarwinFilesOnlyDirectoryExplainsPrivateStageRequirement(t *testing.T) {
	const childEnv = "WAGO_DARWIN_FILES_ONLY_CHILD"
	if os.Getenv(childEnv) == "1" {
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_DARWIN_FILES_ONLY_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_DARWIN_FILES_ONLY_OUTPUT")}, nil,
		))
		return
	}

	dir := t.TempDir()
	inputDir := t.TempDir()
	input := filepath.Join(inputDir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.wago")
	original := []byte("existing runnable artifact")
	if err := os.WriteFile(output, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := exec.Command("/bin/chmod", "+a", "everyone deny add_subdirectory", dir).CombinedOutput(); err != nil {
		t.Skipf("Darwin add-subdirectory ACL unavailable: %v: %s", err, result)
	}
	t.Cleanup(func() { _ = exec.Command("/bin/chmod", "-N", dir).Run() })
	// Prove this directory still permits ordinary direct-child file writes.
	control := filepath.Join(dir, "direct-control")
	file, err := os.OpenFile(control, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Skipf("filesystem does not allow files-only creation: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	controlDirectory := filepath.Join(dir, "subdirectory-control")
	if err := os.Mkdir(controlDirectory, 0o700); err == nil {
		_ = os.Remove(controlDirectory)
		t.Skip("filesystem does not deny subdirectory creation for this identity")
	} else if !os.IsPermission(err) {
		t.Fatalf("subdirectory control: %v", err)
	}

	child := exec.Command(os.Args[0], "-test.run=^TestBuildDarwinFilesOnlyDirectoryExplainsPrivateStageRequirement$")
	child.Env = append(os.Environ(), childEnv+"=1", "WAGO_DARWIN_FILES_ONLY_INPUT="+input,
		"WAGO_DARWIN_FILES_ONLY_OUTPUT="+output)
	combined, err := child.CombinedOutput()
	if err == nil || !strings.Contains(string(combined), "requires permission to create a private staging subdirectory") {
		t.Fatalf("files-only parent error = %v: %s", err, combined)
	}
	if got, err := os.ReadFile(output); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("failed build changed prior artifact to %q, %v", got, err)
	}
	assertNoAtomicBuildTemps(t, dir)
}

func TestBuildNewDarwinOutputPreservesInheritedACLUnderRestrictiveUmask(t *testing.T) {
	const childEnv = "WAGO_DARWIN_ACL_UMASK_CHILD"
	if os.Getenv(childEnv) == "1" {
		syscall.Umask(0o777)
		control := os.Getenv("WAGO_DARWIN_ACL_CONTROL")
		file, err := os.OpenFile(control, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		Command(testEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_DARWIN_ACL_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_DARWIN_ACL_OUTPUT")}, nil,
		))
		return
	}

	dir := t.TempDir()
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read,file_inherit", dir).CombinedOutput(); err != nil {
		t.Skipf("Darwin inheritable ACL unavailable: %v: %s", err, output)
	}
	inputDir := t.TempDir()
	input := filepath.Join(inputDir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(dir, "direct-control")
	output := filepath.Join(dir, "output.wago")
	child := exec.Command(os.Args[0], "-test.run=^TestBuildNewDarwinOutputPreservesInheritedACLUnderRestrictiveUmask$")
	child.Env = append(os.Environ(), childEnv+"=1", "WAGO_DARWIN_ACL_INPUT="+input,
		"WAGO_DARWIN_ACL_CONTROL="+control, "WAGO_DARWIN_ACL_OUTPUT="+output)
	if combined, err := child.CombinedOutput(); err != nil {
		t.Fatalf("restrictive-umask ACL build failed: %v: %s", err, combined)
	}
	controlACL := readDarwinTestAccessACL(t, control)
	outputACL := readDarwinTestAccessACL(t, output)
	if !strings.Contains(controlACL, "everyone") || controlACL != outputACL {
		t.Fatalf("restrictive-umask ACL differs from direct creation: control=%q output=%q", controlACL, outputACL)
	}
	controlInfo, controlErr := os.Stat(control)
	outputInfo, outputErr := os.Stat(output)
	if controlErr != nil || outputErr != nil || controlInfo.Mode().Perm() != 0 || outputInfo.Mode().Perm() != 0 {
		t.Fatalf("restrictive-umask modes: control=%v/%v output=%v/%v",
			controlInfo, controlErr, outputInfo, outputErr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wago-") {
			t.Fatalf("restrictive-umask build left temporary entry %s", entry.Name())
		}
	}
}
