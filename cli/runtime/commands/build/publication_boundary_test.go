package build

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
)

func TestBuildPublicationBoundaryRejectsDestinationChanges(t *testing.T) {
	for _, initiallyExists := range []bool{true, false} {
		name := "missing-to-created"
		if initiallyExists {
			name = "existing-replaced"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			destination := filepath.Join(dir, "artifact.wago")
			preserved := filepath.Join(dir, "preserved-artifact.wago")
			var expected os.FileInfo
			if initiallyExists {
				if err := os.WriteFile(destination, []byte("original artifact"), 0o600); err != nil {
					t.Fatal(err)
				}
				file, err := os.Open(destination)
				if err != nil {
					t.Fatal(err)
				}
				expected, err = file.Stat()
				if statErr := errors.Join(err, file.Close()); statErr != nil {
					t.Fatal(statErr)
				}
			}

			options := atomicfile.Options{Mode: 0o600, Sync: true, Hooks: &atomicfile.Hooks{
				Sync: func(file *os.File) error {
					if err := file.Sync(); err != nil {
						return err
					}
					if initiallyExists {
						if err := os.Rename(destination, preserved); err != nil {
							return err
						}
					}
					return os.WriteFile(destination, []byte("concurrent artifact"), 0o600)
				},
			}}
			setOptionalAtomicfileOption(&options, "BeforeReplace", func(path string) error {
				current, err := os.Lstat(path)
				if !initiallyExists {
					if os.IsNotExist(err) {
						return nil
					}
					if err != nil {
						return err
					}
					return errors.New("output appeared before publication")
				}
				if err != nil {
					return err
				}
				if !os.SameFile(expected, current) {
					return errors.New("output changed before publication")
				}
				return nil
			})

			err := atomicfile.ReplaceFile(destination, options, func(writer io.Writer) error {
				_, err := io.WriteString(writer, "new compiled artifact")
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "before publication") {
				t.Fatalf("publication after destination change = %v", err)
			}
			if got, err := os.ReadFile(destination); err != nil || string(got) != "concurrent artifact" {
				t.Fatalf("concurrent destination = %q, %v", got, err)
			}
			if initiallyExists {
				if got, err := os.ReadFile(preserved); err != nil || string(got) != "original artifact" {
					t.Fatalf("preserved original = %q, %v", got, err)
				}
			}
			assertNoAtomicBuildTemps(t, dir)
		})
	}
}

func TestBuildAtomicPublicationRequiresExistingParent(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "removed-parent")
	destination := filepath.Join(parent, "artifact.wago")
	options := atomicfile.Options{Mode: 0o644}
	setOptionalAtomicfileOption(&options, "ModeSet", true)
	setOptionalAtomicfileOption(&options, "ApplyUmask", true)
	setOptionalAtomicfileOption(&options, "RequireExistingParent", true)

	err := atomicfile.ReplaceFile(destination, options, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "new compiled artifact")
		return err
	})
	if err == nil {
		t.Fatal("publication unexpectedly created its missing parent")
	}
	if _, statErr := os.Stat(parent); !os.IsNotExist(statErr) {
		t.Fatalf("publication created missing parent: %v", statErr)
	}
}

func setOptionalAtomicfileOption(options *atomicfile.Options, name string, value any) {
	// Optional lookup keeps this behavioral regression runnable against the
	// implementation before the production option exists: absence deliberately
	// exercises the unsafe old behavior instead of turning the red commit into a
	// compile failure.
	field := reflect.ValueOf(options).Elem().FieldByName(name)
	if field.IsValid() {
		field.Set(reflect.ValueOf(value))
	}
}

func assertNoAtomicBuildTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wago-atomic-") {
			t.Fatalf("publication left staging file %s", entry.Name())
		}
	}
}
