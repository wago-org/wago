//go:build darwin

package atomicfile

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReplaceFileDarwinStageDoesNotInheritReadableACL(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "artifact.wago")
	if err := os.WriteFile(destination, []byte("previous artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read,file_inherit", dir).CombinedOutput(); err != nil {
		t.Skipf("Darwin inheritable ACL unavailable: %v: %s", err, output)
	}
	probe := filepath.Join(dir, "acl-probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if acl := darwinAtomicTestACL(t, probe); !strings.Contains(acl, "everyone") {
		t.Skipf("filesystem did not inherit the test ACL: %q", acl)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	options := Options{Mode: 0o600, Sync: true}
	value := reflect.ValueOf(&options).Elem()
	for _, name := range []string{"ModeSet", "RetainReplaceHandle"} {
		if field := value.FieldByName(name); field.IsValid() {
			field.SetBool(true)
		}
	}
	callbackReached := false
	err := ReplaceFile(destination, options, func(writer io.Writer) error {
		callbackReached = true
		file, ok := writer.(*os.File)
		if !ok {
			return errors.New("unexpected non-file writer")
		}
		if acl := darwinAtomicTestACL(t, file.Name()); strings.Contains(acl, "everyone") {
			return errors.New("staging file inherited a readable ACL")
		}
		_, err := io.WriteString(writer, "complete artifact")
		return err
	})
	if callbackReached && err != nil && strings.Contains(err.Error(), "inherited a readable ACL") {
		t.Fatalf("artifact bytes would be exposed through inherited staging ACL: %v", err)
	}
	if err != nil {
		// Fail-closed is acceptable when this filesystem cannot construct and
		// validate an ACL-free private stage before exposing the writer.
		if !callbackReached && strings.Contains(err.Error(), "private atomic") {
			assertNoDarwinAtomicTemps(t, dir)
			return
		}
		t.Fatal(err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "complete artifact" {
		t.Fatalf("published destination = %q, %v", got, err)
	}
	if acl := darwinAtomicTestACL(t, destination); strings.Contains(acl, "everyone") {
		t.Fatalf("existing output inherited its parent ACL: %q", acl)
	}
	assertNoDarwinAtomicTemps(t, dir)
}

func TestReplaceFileDarwinRejectsParentReplacement(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "current")
	detached := filepath.Join(root, "detached")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "artifact.wago")
	if err := os.WriteFile(destination, []byte("previous artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := Options{Mode: 0o600, Sync: true, Hooks: &Hooks{Sync: func(file *os.File) error {
		if err := file.Sync(); err != nil {
			return err
		}
		if err := os.Rename(parent, detached); err != nil {
			return err
		}
		return os.Mkdir(parent, 0o700)
	}}}
	value := reflect.ValueOf(&options).Elem()
	for _, name := range []string{"ModeSet", "RetainReplaceHandle"} {
		if field := value.FieldByName(name); field.IsValid() {
			field.SetBool(true)
		}
	}
	err := ReplaceFile(destination, options, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "new artifact")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "parent changed") {
		t.Fatalf("replacement after parent swap = %v", err)
	}
	oldDestination := filepath.Join(detached, filepath.Base(destination))
	if got, err := os.ReadFile(oldDestination); err != nil || string(got) != "previous artifact" {
		t.Fatalf("detached destination = %q, %v", got, err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("replacement appeared in new parent: %v", err)
	}
	assertNoDarwinAtomicTemps(t, detached)
}

func TestReplaceFileDarwinPinsMissingStageThroughPublication(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "artifact.wago")
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
	value := reflect.ValueOf(&options).Elem()
	for _, name := range []string{"ModeSet", "RetainReplaceHandle"} {
		if field := value.FieldByName(name); field.IsValid() {
			field.SetBool(true)
		}
	}
	err := ReplaceFile(destination, options, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "complete artifact")
		return err
	})
	if err == nil {
		t.Fatal("replacement accepted an exchanged temporary file")
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("missing destination was published: %v", statErr)
	}
	if got, readErr := os.ReadFile(temporary); readErr != nil || string(got) != "substituted artifact" {
		t.Fatalf("exchanged temporary entry = %q, %v", got, readErr)
	}
	if got, readErr := os.ReadFile(detached); readErr != nil || string(got) != "complete artifact" {
		t.Fatalf("detached original temporary = %q, %v", got, readErr)
	}
}

func darwinAtomicTestACL(t *testing.T, path string) string {
	t.Helper()
	output, err := exec.Command("/bin/ls", "-led", path).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect Darwin ACL for %s: %v: %s", path, err, output)
	}
	return string(output)
}

func assertNoDarwinAtomicTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".wago-atomic-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("atomic temporary entries remain: %v", matches)
	}
}
