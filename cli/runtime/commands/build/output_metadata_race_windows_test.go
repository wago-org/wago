//go:build windows

package build

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
	"golang.org/x/sys/windows"
)

func TestBuildSnapshotCallbackRejectsDACLChange(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output.wago")
	if err := os.WriteFile(output, []byte("original artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if outputMetadata.accessPolicyDescriptor == nil {
		t.Fatal("captured metadata has no resource-attribute/central-policy descriptor")
	}
	options := atomicfile.Options{
		Mode: 0o600, ModeSet: true, Sync: true,
		BeforeReplace: func(destination string) error {
			return snapshot.validateBeforeReplace(destination, outputInfo, outputMetadata)
		},
		Hooks: &atomicfile.Hooks{Sync: func(file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			setWindowsBoundaryDistinctDACL(t, output)
			if windowsBoundarySecurityDescriptor(t, output) == windowsBoundarySecurityDescriptorFromMetadata(t, outputMetadata) {
				t.Skip("filesystem did not retain a distinct test DACL")
			}
			return nil
		}},
	}
	err = atomicfile.ReplaceFile(publicationPath, options, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "new compiled artifact")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "metadata changed before publication") {
		t.Fatalf("snapshot callback after DACL change = %v", err)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != "original artifact" {
		t.Fatalf("output after rejected publication = %q, %v", got, err)
	}
	assertNoAtomicBuildTemps(t, dir)
}

func windowsBoundarySecurityDescriptorFromMetadata(t *testing.T, metadata buildOutputMetadata) string {
	t.Helper()
	if metadata.descriptor == nil {
		t.Fatal("captured metadata has no security descriptor")
	}
	return metadata.descriptor.String()
}

func TestSameBuildOutputMetadataIncludesAllWindowsAttributes(t *testing.T) {
	left := buildOutputMetadata{fileAttributes: windows.FILE_ATTRIBUTE_ARCHIVE}
	right := buildOutputMetadata{fileAttributes: windows.FILE_ATTRIBUTE_ARCHIVE | windows.FILE_ATTRIBUTE_HIDDEN}
	if sameBuildOutputMetadata(left, right) {
		t.Fatal("metadata with distinct Windows attributes compared equal")
	}
}

func TestSameWindowsFilteredSACLDescriptorNormalizesEmptyForms(t *testing.T) {
	withoutSACL, err := windows.SecurityDescriptorFromString("O:SY")
	if err != nil {
		t.Fatal(err)
	}
	emptySACL, err := windows.SecurityDescriptorFromString("O:SYS:")
	if err != nil {
		t.Fatal(err)
	}
	if sameWindowsSecurityDescriptor(withoutSACL, emptySACL) {
		t.Fatal("raw descriptor comparison unexpectedly normalized distinct encodings")
	}
	if !sameWindowsFilteredSACLDescriptor(withoutSACL, emptySACL) {
		t.Fatal("filtered SACL comparison rejected equivalent empty encodings")
	}
	label, err := windows.SecurityDescriptorFromString("S:(ML;;NW;;;LW)")
	if err != nil {
		t.Fatal(err)
	}
	if sameWindowsFilteredSACLDescriptor(emptySACL, label) {
		t.Fatal("filtered SACL comparison accepted a nonempty mandatory label")
	}
}
