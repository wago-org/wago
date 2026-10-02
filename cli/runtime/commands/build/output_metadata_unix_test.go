//go:build unix

package build

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func TestSameBuildOutputMetadataIncludesAccessMetadata(t *testing.T) {
	baseline := buildOutputMetadata{
		set: true, uid: 1000, gid: 1000,
		accessACL: []byte("acl"), accessACLPresent: true,
		securityLabels: []buildOutputSecurityLabel{{name: "security.example", value: []byte("domain"), present: true}},
	}
	identical := buildOutputMetadata{
		set: true, uid: 1000, gid: 1000,
		accessACL: append([]byte(nil), baseline.accessACL...), accessACLPresent: true,
		securityLabels: []buildOutputSecurityLabel{{name: "security.example", value: append([]byte(nil), baseline.securityLabels[0].value...), present: true}},
	}
	if !sameBuildOutputMetadata(baseline, identical) {
		t.Fatal("identical metadata compared different")
	}
	mutations := []buildOutputMetadata{
		{set: true, uid: 1001, gid: 1000, accessACL: []byte("acl"), accessACLPresent: true, securityLabels: identical.securityLabels},
		{set: true, uid: 1000, gid: 1001, accessACL: []byte("acl"), accessACLPresent: true, securityLabels: identical.securityLabels},
		{set: true, uid: 1000, gid: 1000, accessACL: []byte("other"), accessACLPresent: true, securityLabels: identical.securityLabels},
		{set: true, uid: 1000, gid: 1000, accessACL: []byte("acl"), accessACLPresent: true, securityLabels: []buildOutputSecurityLabel{{name: "security.example", value: []byte("other"), present: true}}},
	}
	for _, mutation := range mutations {
		if sameBuildOutputMetadata(baseline, mutation) {
			t.Fatalf("different metadata compared equal: %+v", mutation)
		}
	}
	if !bytes.Equal(baseline.accessACL, []byte("acl")) {
		t.Fatal("metadata comparison mutated input")
	}
}

func TestBuildPreservesUnixOutputOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing an artifact to a distinct owner requires root")
	}
	for _, throughSymlink := range []bool{false, true} {
		name := "direct"
		if throughSymlink {
			name = "symlink-target"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.wasm")
			if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "artifact.wago")
			if err := os.WriteFile(target, []byte("old artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			const uid, gid = 1, 1
			if err := os.Chown(target, uid, gid); err != nil {
				t.Skipf("changing artifact ownership is unavailable: %v", err)
			}
			output := target
			if throughSymlink {
				output = filepath.Join(dir, "output.wago")
				if err := os.Symlink(filepath.Base(target), output); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}

			Command(testEnvironment{}).Run(command.NewContext(
				[]string{input}, map[string]string{"output": output}, nil,
			))

			artifact, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !wago.IsCompiled(artifact) {
				t.Fatalf("owned target is not a compiled artifact: %x", artifact)
			}
			info, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if stat.Uid != uid || stat.Gid != gid {
				t.Fatalf("target owner = %d:%d, want %d:%d", stat.Uid, stat.Gid, uid, gid)
			}
			if throughSymlink {
				requireTestSymlink(t, output)
			}
		})
	}
}
