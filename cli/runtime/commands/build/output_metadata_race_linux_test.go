//go:build linux

package build

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
	"golang.org/x/sys/unix"
)

func TestBuildSnapshotCallbackRejectsAccessACLChange(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output.wago")
	if err := os.WriteFile(output, []byte("original artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	setLinuxTestAccessACL(t, output)
	snapshot, err := inspectBuildOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	publicationPath, _, _, outputInfo, err := snapshot.revalidate()
	if err != nil {
		t.Fatal(err)
	}
	outputMetadata, err := captureBuildOutputMetadata(publicationPath, outputInfo, snapshot.targetIdentity)
	if err != nil {
		t.Fatal(err)
	}
	changedACL := append([]byte(nil), outputMetadata.accessACL...)
	binary.LittleEndian.PutUint16(changedACL[14:], 2)
	options := atomicfile.Options{
		Mode: outputInfo.Mode().Perm(), ModeSet: true, Sync: true,
		BeforeReplace: func(destination string) error {
			return snapshot.validateBeforeReplace(destination, outputInfo, outputMetadata)
		},
		Hooks: &atomicfile.Hooks{Sync: func(file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			return unix.Setxattr(output, testLinuxAccessACLName, changedACL, 0)
		}},
	}
	err = atomicfile.ReplaceFile(publicationPath, options, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "new compiled artifact")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "metadata changed before publication") {
		t.Fatalf("snapshot callback after ACL change = %v", err)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != "original artifact" {
		t.Fatalf("output after rejected publication = %q, %v", got, err)
	}
	assertNoAtomicBuildTemps(t, dir)
}
