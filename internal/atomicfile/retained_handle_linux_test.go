//go:build linux

package atomicfile

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReplaceFileLinuxPinsStageThroughPublication(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "existing"}[existing], func(t *testing.T) {
			dir := t.TempDir()
			destination := filepath.Join(dir, "artifact.wago")
			if existing {
				if err := os.WriteFile(destination, []byte("previous artifact"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			var temporary, detached string
			options := Options{Mode: 0o600, Sync: true, Hooks: &Hooks{Sync: func(file *os.File) error {
				if err := file.Sync(); err != nil {
					return err
				}
				temporary, detached = file.Name(), file.Name()+".detached"
				if err := os.Rename(temporary, detached); err != nil {
					return err
				}
				return os.WriteFile(temporary, []byte("substituted artifact"), 0o600)
			}}}
			setOptionalAtomicfileTestOption(&options, "ModeSet", true)
			setOptionalAtomicfileTestOption(&options, "RetainReplaceHandle", true)

			err := ReplaceFile(destination, options, func(writer io.Writer) error {
				_, err := io.WriteString(writer, "complete artifact")
				return err
			})
			if err == nil {
				t.Fatal("replacement accepted an exchanged temporary file")
			}
			if existing {
				if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "previous artifact" {
					t.Fatalf("existing destination = %q, %v", got, readErr)
				}
			} else if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("missing destination was published: %v", statErr)
			}
			if got, readErr := os.ReadFile(temporary); readErr != nil || string(got) != "substituted artifact" {
				t.Fatalf("exchanged temporary entry = %q, %v", got, readErr)
			}
			if got, readErr := os.ReadFile(detached); readErr != nil || string(got) != "complete artifact" {
				t.Fatalf("detached original temporary = %q, %v", got, readErr)
			}
		})
	}
}

func TestLinuxRetainedCleanupCannotUndoCommittedPublication(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "artifact.wago")
	var privateDir, blocker string
	t.Cleanup(func() {
		_ = os.Remove(blocker)
		_ = os.Remove(privateDir)
	})
	// A same-identity peer can keep the private directory nonempty. That cleanup
	// problem must not make an already-published artifact look like a failed build.
	options := Options{Mode: 0o600, Sync: true, BeforeReplace: func(string) error {
		blocker = filepath.Join(privateDir, "cleanup-blocker")
		return os.WriteFile(blocker, nil, 0o600)
	}}
	setOptionalAtomicfileTestOption(&options, "ModeSet", true)
	setOptionalAtomicfileTestOption(&options, "RetainReplaceHandle", true)
	if err := ReplaceFile(destination, options, func(writer io.Writer) error {
		privateDir = filepath.Dir(writer.(*os.File).Name())
		_, err := io.WriteString(writer, "complete artifact")
		return err
	}); err != nil {
		t.Fatalf("replacement after committed publication = %v", err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "complete artifact" {
		t.Fatalf("published artifact = %q, %v", got, err)
	}
}

// Keep the red test source compatible with the pre-fix Options shape.
func setOptionalAtomicfileTestOption(options *Options, name string, value bool) {
	field := reflect.ValueOf(options).Elem().FieldByName(name)
	if field.IsValid() {
		field.SetBool(value)
	}
}
