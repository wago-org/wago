package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestReplaceFileCreatesAndReplaces(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "value")
	if err := ReplaceFile(path, Options{Mode: 0o640, Sync: true}, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "first")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(path, Options{Mode: 0o640}, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "replacement")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("destination = %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("mode = %v, %v", info, err)
		}
	}
	assertNoTemps(t, directory)
}

func TestReplaceFileFailuresPreserveDestination(t *testing.T) {
	injected := errors.New("injected failure")
	tests := []struct {
		name    string
		write   func(io.Writer) error
		options Options
	}{
		{name: "write", write: func(writer io.Writer) error {
			_, _ = io.WriteString(writer, "secret partial")
			return injected
		}},
		{name: "sync", write: writeString("new"), options: Options{Sync: true, Hooks: &Hooks{Sync: func(file *os.File) error {
			return injected
		}}}},
		{name: "close", write: writeString("new"), options: Options{Hooks: &Hooks{Close: func(file *os.File) error {
			_ = file.Close()
			return injected
		}}}},
		{name: "replace", write: writeString("new"), options: Options{Hooks: &Hooks{Replace: func(_, _ string) error {
			return injected
		}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "value")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			options := test.options
			options.Mode = 0o600
			err := ReplaceFile(path, options, test.write)
			if err == nil {
				t.Fatal("failure was not returned")
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != "old" {
				t.Fatalf("old destination = %q, %v", data, readErr)
			}
			assertNoTemps(t, directory)
		})
	}
}

func TestCloseFailureStillClosesAndCleansTemporary(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "value")
	injected := errors.New("injected close failure")
	var temporary *os.File
	err := ReplaceFile(destination, Options{Hooks: &Hooks{Close: func(file *os.File) error {
		temporary = file
		return injected
	}}}, writeString("new"))
	if !errors.Is(err, injected) {
		t.Fatalf("close failure = %v", err)
	}
	if temporary == nil {
		t.Fatal("close hook did not receive temporary file")
	}
	if _, err := temporary.Stat(); err == nil {
		t.Fatal("temporary descriptor remained open after close failure")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination was published after close failure: %v", err)
	}
	assertNoTemps(t, directory)
}

func TestReplaceFileRejectsDirectoryAndSymlink(t *testing.T) {
	directory := t.TempDir()
	if err := ReplaceFile(directory, Options{}, writeString("x")); err == nil {
		t.Fatal("directory destination accepted")
	}
	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "link")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := ReplaceFile(link, Options{}, writeString("replacement")); err == nil {
		t.Fatal("symlink destination accepted")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "target" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}
}

func TestCreateTempUsesUniqueSameDirectoryNames(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "value")
	const count = 16
	names := make(chan string, count)
	errorsChannel := make(chan error, count)
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			file, err := CreateTemp(destination)
			if err != nil {
				errorsChannel <- err
				return
			}
			name := file.Name()
			_ = file.Close()
			names <- name
			_ = os.Remove(name)
		}()
	}
	wait.Wait()
	close(names)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for name := range names {
		if filepath.Dir(name) != directory || seen[name] {
			t.Fatalf("invalid/non-unique temporary path %q", name)
		}
		seen[name] = true
	}
	if len(seen) != count {
		t.Fatalf("unique names = %d, want %d", len(seen), count)
	}
}

func TestCommitTempFileReplacesExisting(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "value")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	temporary, err := CreateTemp(destination)
	if err != nil {
		t.Fatal(err)
	}
	name := temporary.Name()
	if _, err := temporary.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CommitTempFile(name, destination, Options{Mode: 0o600, Sync: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "new" {
		t.Fatalf("destination = %q, %v", data, err)
	}
	assertNoTemps(t, directory)
}

func FuzzReplaceFileContents(f *testing.F) {
	f.Add([]byte("one"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		directory := t.TempDir()
		path := filepath.Join(directory, "value")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ReplaceFile(path, Options{Mode: 0o600}, func(writer io.Writer) error {
			_, err := writer.Write(data)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(data) {
			t.Fatalf("contents differ: %v", err)
		}
	})
}

func writeString(value string) func(io.Writer) error {
	return func(writer io.Writer) error {
		_, err := io.WriteString(writer, value)
		return err
	}
}

func assertNoTemps(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(directory, ".wago-atomic-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary debris = %v, %v", matches, err)
	}
}

func ExampleReplaceFile() {
	directory, _ := os.MkdirTemp("", "atomic-example-")
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "value")
	_ = ReplaceFile(path, Options{Mode: 0o600}, writeString("complete"))
	data, _ := os.ReadFile(path)
	fmt.Println(string(data))
	// Output: complete
}
