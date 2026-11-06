package registry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
)

func TestCredentialMutationRejectsPreCanceledContextWithoutCreatingStore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WAGO_HOME", root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := saveCredentialsContext(ctx, "https://one.test", "token", "one"); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled credential save = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(credentialsPath())); !os.IsNotExist(err) {
		t.Fatalf("pre-canceled credential save created store: %v", err)
	}
}

func TestCredentialMutationRejectsNullStoreWithoutOverwritingIt(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath(), []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveCredentials("https://one.test", "token", "one"); err == nil {
		t.Fatal("credential save accepted a null store")
	}
	data, err := os.ReadFile(credentialsPath())
	if err != nil || string(data) != "null\n" {
		t.Fatalf("null credential store changed to %q: %v", data, err)
	}
	assertNoCredentialTemps(t)
}

func TestCredentialStoreRejectsDuplicateMembers(t *testing.T) {
	for _, data := range []string{
		`{"https://one.test":{"token":"one","login":"alice"},"https://one.test":{"token":"two","login":"mallory"}}`,
		`{"https://one.test":{"token":"one","Token":"two","login":"alice"}}`,
	} {
		t.Run(data[:min(len(data), 40)], func(t *testing.T) {
			t.Setenv("WAGO_HOME", t.TempDir())
			if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(credentialsPath(), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadCredentials(); err == nil {
				t.Fatal("credential store with duplicate members succeeded")
			}
		})
	}
}

func TestCredentialStorePreservesCaseSensitiveRegistryPaths(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"https://one.test/A":{"token":"one","login":"alice"},"https://one.test/a":{"token":"two","login":"bob"}}`
	if err := os.WriteFile(credentialsPath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	creds, err := loadCredentials()
	if err != nil || len(creds) != 2 || creds["https://one.test/A"].Token != "one" || creds["https://one.test/a"].Token != "two" {
		t.Fatalf("case-sensitive credential keys = %#v, %v", creds, err)
	}
}

func TestCredentialStoreReadAndWriteAreBounded(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	oversized := strings.Repeat("x", int(maximumCredentialStoreSize)+1)
	if err := os.WriteFile(credentialsPath(), []byte(oversized), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCredentials(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized credential read = %v", err)
	}

	if err := os.WriteFile(credentialsPath(), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	creds := map[string]credential{
		"https://oversized.test": {Token: oversized, Login: "alice"},
	}
	if err := writeCredentials(creds); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized credential write = %v", err)
	}
	data, err := os.ReadFile(credentialsPath())
	if err != nil || string(data) != "{}\n" {
		t.Fatalf("oversized write changed store to %q: %v", data, err)
	}
}

func TestCredentialStoreRejectsNonRegularPath(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	if err := os.MkdirAll(credentialsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCredentials(); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory credential store = %v", err)
	}
}

func TestCredentialMutationRejectsSymlinkDestination(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.json")
	original := []byte("{}\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, credentialsPath()); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := saveCredentials("https://one.test", "token", "one"); err == nil {
		t.Fatal("credential save accepted a symlink destination")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(original) {
		t.Fatalf("credential symlink target changed to %q: %v", data, err)
	}
	assertNoCredentialTemps(t)
}

func TestCredentialUpdateRepairsPrivateMode(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WAGO_HOME", root)
	if err := saveCredentials("https://one.test", "token-one", "one"); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Chmod(credentialsPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveCredentials("https://two.test", "token-two", "two"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(credentialsPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %v, %v", info, err)
	}
	directory, err := os.Stat(filepath.Dir(credentialsPath()))
	if err != nil || directory.Mode().Perm() != 0o700 {
		t.Fatalf("credential directory mode = %v, %v", directory, err)
	}
}

func TestConcurrentCredentialMutationsPreserveUnrelatedEntries(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	var wait sync.WaitGroup
	errorsChannel := make(chan error, 2)
	for _, item := range []struct{ base, token string }{
		{"https://one.test", "token-one"},
		{"https://two.test", "token-two"},
	} {
		item := item
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsChannel <- saveCredentials(item.base, item.token, item.base)
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	creds, err := loadCredentials()
	if err != nil || creds["https://one.test"].Token != "token-one" || creds["https://two.test"].Token != "token-two" {
		t.Fatalf("credentials = %#v, %v", creds, err)
	}

	if err := saveCredentials("https://delete.test", "delete", "delete"); err != nil {
		t.Fatal(err)
	}
	errorsChannel = make(chan error, 2)
	wait.Add(2)
	go func() {
		defer wait.Done()
		errorsChannel <- saveCredentials("https://one.test", "updated", "one")
	}()
	go func() {
		defer wait.Done()
		errorsChannel <- deleteCredentials("https://delete.test")
	}()
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	creds, err = loadCredentials()
	if err != nil || creds["https://one.test"].Token != "updated" {
		t.Fatalf("serialized save/delete = %#v, %v", creds, err)
	}
	if _, exists := creds["https://delete.test"]; exists {
		t.Fatal("concurrent delete was lost")
	}
}

func TestCredentialProcessesSerialize(t *testing.T) {
	if os.Getenv("WAGO_TEST_CREDENTIAL_HELPER") == "1" {
		if err := saveCredentials(os.Getenv("WAGO_TEST_BASE"), os.Getenv("WAGO_TEST_TOKEN"), "helper"); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	const count = 4
	commands := make([]*exec.Cmd, count)
	outputs := make([]bytes.Buffer, count)
	for index := range count {
		commands[index] = exec.Command(os.Args[0], "-test.run=^TestCredentialProcessesSerialize$")
		commands[index].Env = append(os.Environ(),
			"WAGO_TEST_CREDENTIAL_HELPER=1",
			"WAGO_HOME="+root,
			"WAGO_TEST_BASE=https://registry-"+string(rune('a'+index))+".test",
			"WAGO_TEST_TOKEN=token-"+string(rune('a'+index)),
		)
		commands[index].Stdout = &outputs[index]
		commands[index].Stderr = &outputs[index]
		if err := commands[index].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("credential helper: %v\n%s", err, outputs[index].String())
		}
	}
	t.Setenv("WAGO_HOME", root)
	creds, err := loadCredentials()
	if err != nil || len(creds) != count {
		t.Fatalf("cross-process credentials = %#v, %v", creds, err)
	}
}

func TestCredentialPublicationFailuresPreserveOldFile(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	if err := saveCredentials("https://old.test", "old-token", "old"); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(credentialsPath())
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected credential publication failure")
	tests := []struct {
		name    string
		replace func(string, atomicfile.Options, func(io.Writer) error) error
		hooks   *atomicfile.Hooks
	}{
		{name: "write", replace: func(path string, options atomicfile.Options, write func(io.Writer) error) error {
			return atomicfile.ReplaceFile(path, options, func(writer io.Writer) error {
				_, _ = io.WriteString(writer, "token-bearing partial")
				return injected
			})
		}},
		{name: "sync", hooks: &atomicfile.Hooks{Sync: func(*os.File) error { return injected }}},
		{name: "close", hooks: &atomicfile.Hooks{Close: func(file *os.File) error {
			_ = file.Close()
			return injected
		}}},
		{name: "replace", hooks: &atomicfile.Hooks{Replace: func(_, _ string) error { return injected }}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oldReplace, oldHooks := replaceCredentialFile, credentialAtomicHooks
			t.Cleanup(func() {
				replaceCredentialFile, credentialAtomicHooks = oldReplace, oldHooks
			})
			if test.replace != nil {
				replaceCredentialFile = test.replace
			}
			credentialAtomicHooks = test.hooks
			err := saveCredentials("https://new.test", "super-secret-token", "new")
			if err == nil {
				t.Fatal("injected failure was not returned")
			}
			if strings.Contains(err.Error(), "super-secret-token") {
				t.Fatalf("error leaked token: %v", err)
			}
			got, readErr := os.ReadFile(credentialsPath())
			if readErr != nil || string(got) != string(original) {
				t.Fatalf("old credentials changed: %q, %v", got, readErr)
			}
			assertNoCredentialTemps(t)
		})
	}
}

func TestCredentialTemporaryFileIsPrivateAndReadersSeeCompleteJSON(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	oldReplace := replaceCredentialFile
	t.Cleanup(func() { replaceCredentialFile = oldReplace })
	checked := false
	replaceCredentialFile = func(path string, options atomicfile.Options, write func(io.Writer) error) error {
		previous, readErr := os.ReadFile(path)
		missing := errors.Is(readErr, os.ErrNotExist)
		if readErr != nil && !missing {
			t.Fatal(readErr)
		}
		return atomicfile.ReplaceFile(path, options, func(writer io.Writer) error {
			if file, ok := writer.(*os.File); ok && runtime.GOOS != "windows" {
				info, err := file.Stat()
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("temporary credential mode = %v, %v", info, err)
				}
				checked = true
			}
			// Inspect the published path while the temporary contains only half
			// the JSON. These checkpoints exercise publication without racing
			// Windows file-open and replacement semantics.
			return write(credentialPublicationWriter{writer, func() {
				t.Helper()
				got, err := os.ReadFile(path)
				if missing {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("new credentials published before write completed: %v", err)
					}
				} else if err != nil || !bytes.Equal(got, previous) {
					t.Fatalf("old credentials changed before write completed: %v", err)
				}
			}})
		})
	}
	checkPublished := func(token string) {
		t.Helper()
		creds, err := loadCredentials()
		if err != nil || len(creds) != 1 || creds["https://one.test"].Token != token || creds["https://one.test"].Login != "one" {
			t.Fatalf("published credentials are incomplete or stale: %v", err)
		}
	}
	if err := saveCredentials("https://one.test", "one", "one"); err != nil {
		t.Fatal(err)
	}
	checkPublished("one")
	if runtime.GOOS != "windows" && !checked {
		t.Fatal("temporary credential file was not inspected")
	}

	for index := range 50 {
		token := strings.Repeat("x", 1024) + string(rune('A'+index%26))
		if err := saveCredentials("https://one.test", token, "one"); err != nil {
			t.Fatal(err)
		}
		checkPublished(token)
	}
	assertNoCredentialTemps(t)
}

type credentialPublicationWriter struct {
	io.Writer
	checkpoint func()
}

func (w credentialPublicationWriter) Write(data []byte) (int, error) {
	half := len(data) / 2
	n, err := w.Writer.Write(data[:half])
	if err != nil {
		return n, err
	}
	if n != half {
		return n, io.ErrShortWrite
	}
	w.checkpoint()
	m, err := w.Writer.Write(data[half:])
	if err == nil && m != len(data)-half {
		err = io.ErrShortWrite
	}
	return n + m, err
}

func assertNoCredentialTemps(t *testing.T) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(credentialsPath()), ".wago-atomic-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("credential temporary debris = %v, %v", matches, err)
	}
}
