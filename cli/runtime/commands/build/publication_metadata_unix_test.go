//go:build unix

package build

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
)

func TestBuildPublicationBoundaryRejectsModeChange(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "artifact.wago")
	if err := os.WriteFile(destination, []byte("original artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}

	options := atomicfile.Options{Mode: 0o600, Sync: true, Hooks: &atomicfile.Hooks{
		Sync: func(file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			return os.Chmod(destination, 0o640)
		},
	}}
	setOptionalAtomicfileOption(&options, "BeforeReplace", func(path string) error {
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(expected, current) {
			return errors.New("output changed before publication")
		}
		if current.Mode() != expected.Mode() {
			return errors.New("output metadata changed before publication")
		}
		return nil
	})

	err = atomicfile.ReplaceFile(destination, options, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "new compiled artifact")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "metadata changed before publication") {
		t.Fatalf("publication after mode change = %v", err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "original artifact" {
		t.Fatalf("destination after rejected publication = %q, %v", got, err)
	}
	if info, err := os.Lstat(destination); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("concurrent destination mode = %v, %v", info.Mode(), err)
	}
	assertNoAtomicBuildTemps(t, dir)
}
