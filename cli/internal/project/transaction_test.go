package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
)

func TestMetadataTransactionRecoversCommittedSplitWrite(t *testing.T) {
	dir := t.TempDir()
	initialManifest := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{}}
	if err := Write(dir, initialManifest); err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(dir, NewLockDocument()); err != nil {
		t.Fatal(err)
	}

	id := "github.com/acme/pool"
	manifest := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{id: "^1.0.0"}}
	lock := NewLockDocument()
	lock.Plugins[id] = testLockEntry(true, id, map[string]string{})

	previous := replaceProjectFile
	t.Cleanup(func() { replaceProjectFile = previous })
	failed := false
	replaceProjectFile = func(destination string, options atomicfile.Options, write func(io.Writer) error) error {
		if destination == LockPath(dir) && !failed {
			failed = true
			return errors.New("injected lock replacement failure")
		}
		return atomicfile.ReplaceFile(destination, options, write)
	}
	err := WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		return mutation.PublishMetadata(manifest, lock)
	})
	replaceProjectFile = previous
	if err == nil || !TransactionCommitted(err) {
		t.Fatalf("split publication error = %v, committed = %v", err, TransactionCommitted(err))
	}
	if _, statErr := os.Stat(projectJournalPath(dir)); statErr != nil {
		t.Fatalf("committed journal is absent: %v", statErr)
	}

	gotManifest, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotLock, err := ReadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotManifest, manifest) || !reflect.DeepEqual(gotLock, lock) {
		t.Fatalf("recovered metadata = %#v, %#v; want %#v, %#v", gotManifest, gotLock, manifest, lock)
	}
	if _, statErr := os.Stat(projectJournalPath(dir)); !os.IsNotExist(statErr) {
		t.Fatalf("completed journal remains: %v", statErr)
	}
}

func TestMetadataTransactionRejectsInconsistentPairBeforeCommit(t *testing.T) {
	dir := t.TempDir()
	manifest := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{"github.com/acme/pool": "^1.0.0"}}
	err := WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		return mutation.PublishMetadata(manifest, NewLockDocument())
	})
	if err == nil || TransactionCommitted(err) {
		t.Fatalf("inconsistent publication error = %v, committed = %v", err, TransactionCommitted(err))
	}
	if _, statErr := os.Stat(projectJournalPath(dir)); !os.IsNotExist(statErr) {
		t.Fatalf("inconsistent transaction wrote journal: %v", statErr)
	}
	if _, statErr := os.Stat(Path(dir)); !os.IsNotExist(statErr) {
		t.Fatalf("inconsistent transaction wrote manifest: %v", statErr)
	}
}

func TestMetadataReadDoesNotRequireOrCreateProjectState(t *testing.T) {
	dir := t.TempDir()
	manifest := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{}}
	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir), data, 0o444); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, manifest) {
		t.Fatalf("read-only manifest = %#v, want %#v", got, manifest)
	}
	if _, err := os.Stat(filepath.Join(dir, projectDirectory)); !os.IsNotExist(err) {
		t.Fatalf("metadata read created project state: %v", err)
	}
}

func TestMetadataTransactionPreservesExistingFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	dir := t.TempDir()
	id := "github.com/acme/pool"
	manifest := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{id: "^1.0.0"}}
	lock := NewLockDocument()
	lock.Plugins[id] = testLockEntry(true, id, map[string]string{})
	initialManifest, err := EncodeManifest(map[string]any{"$schema": SchemaURI, "plugins": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	initialLock, err := EncodeLock(NewLockDocument())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir), initialManifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LockPath(dir), initialLock, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WithMutation(context.Background(), dir, func(mutation *Mutation) error {
		return mutation.PublishMetadata(manifest, lock)
	}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{Path(dir): 0o600, LockPath(dir): 0o640} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %04o, want %04o", path, got, want)
		}
	}
}

func TestMetadataTransactionPreservesPriorFileWhenJournalCommitFails(t *testing.T) {
	dir := t.TempDir()
	want := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{}}
	if err := Write(dir, want); err != nil {
		t.Fatal(err)
	}
	previous := replaceProjectFile
	t.Cleanup(func() { replaceProjectFile = previous })
	replaceProjectFile = func(destination string, options atomicfile.Options, write func(io.Writer) error) error {
		if destination == projectJournalPath(dir) {
			return errors.New("injected journal failure")
		}
		return atomicfile.ReplaceFile(destination, options, write)
	}
	err := Write(dir, map[string]any{"$schema": SchemaURI, "plugins": map[string]any{}, "settings": map[string]any{"runtime": map[string]any{"parallel": "2"}}})
	replaceProjectFile = previous
	if err == nil || TransactionCommitted(err) {
		t.Fatalf("journal failure = %v, committed = %v", err, TransactionCommitted(err))
	}
	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest after failed journal = %#v, want %#v", got, want)
	}
}

func TestMetadataTransactionRejectsCorruptRecoveryJournal(t *testing.T) {
	dir := t.TempDir()
	want := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{}}
	if err := Write(dir, want); err != nil {
		t.Fatal(err)
	}
	journal := projectJournal{
		FormatVersion: projectTransactionFormat,
		Manifest: &journalFile{
			Data:   []byte(`{"$schema":"https://wago.sh/v1/schema.json","plugins":{}}`),
			SHA256: "not-the-data-checksum",
		},
	}
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectJournalPath(dir), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Fatal("Read accepted a corrupt project transaction journal")
	}
	data, err = os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeManifest(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("corrupt journal changed manifest: %#v", decoded)
	}
}

func TestConcurrentDependencyUpdatesAreSerialized(t *testing.T) {
	dir := t.TempDir()
	if _, err := Initialize(dir); err != nil {
		t.Fatal(err)
	}
	const count = 24
	var wait sync.WaitGroup
	errs := make(chan error, count)
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			id := fmt.Sprintf("example.com/plugins/plugin-%02d", index)
			added, err := AddDependency(dir, id, "^1.0.0")
			if err == nil && !added {
				err = fmt.Errorf("%s was not added", id)
			}
			errs <- err
		}(index)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	requirements, err := Requirements(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != count {
		t.Fatalf("serialized dependencies = %d, want %d", len(requirements), count)
	}
}
