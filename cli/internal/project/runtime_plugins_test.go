//go:build !wago_minimal

package project

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

func TestPluginSelectionsIncludesDirectAndTransitiveLockedGraph(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"$schema":"https://wago.sh/v1/schema.json","plugins":{"github.com/acme/pool":"^1.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := NewLockDocument()
	pool := testLockEntry(true, "github.com/acme/pool", map[string]string{"github.com/acme/workers": "^1.0.0"})
	workers := testLockEntry(false, "github.com/acme/workers", map[string]string{})
	workers.Grants = []AuthorityGrant{{Name: "instance.manage", Scope: AuthorityScope{MaxInstances: 2, MaxMemoryBytes: 4096}}}
	workers.RequestedAuthorities = []AuthorityRequest{{Name: "instance.manage", Mode: AuthorityOptional, Reason: "own workers", Scope: AuthorityScope{MaxInstances: 4, MaxMemoryBytes: 8192}}}
	workers.Config = json.RawMessage(`{"workers":2}`)
	lock.Plugins["github.com/acme/pool"] = pool
	lock.Plugins["github.com/acme/workers"] = workers
	if err := WriteLock(dir, lock); err != nil {
		t.Fatal(err)
	}
	got, err := PluginSelections(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "github.com/acme/pool" || got[1].ID != "github.com/acme/workers" {
		t.Fatalf("selections = %#v", got)
	}
	if !got[0].Direct || got[1].Direct || !reflect.DeepEqual(got[0].Dependencies, pool.Dependencies) || len(got[1].Dependencies) != 0 {
		t.Fatalf("selection roots/dependencies = %#v", got)
	}
	if !reflect.DeepEqual(got[1].Grants, workers.Grants) || !jsonEqual(got[1].Config, workers.Config) {
		t.Fatalf("workers selection = %#v", got[1])
	}
}

func TestPluginSelectionsRequiresDirectResolution(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"$schema":"https://wago.sh/v1/schema.json","plugins":{"github.com/acme/missing":"^1.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PluginSelections(dir); err == nil {
		t.Fatal("PluginSelections accepted an unresolved direct requirement")
	}
}

func TestPluginSelectionsFreshReadDoesNotCreateProjectState(t *testing.T) {
	dir := t.TempDir()
	manifest := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{"github.com/acme/alpha": "^1.0.0"}}
	lock := NewLockDocument()
	lock.Plugins["github.com/acme/alpha"] = testLockEntry(true, "github.com/acme/alpha", map[string]string{})
	manifestData, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	lockData, err := EncodeLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir), manifestData, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LockPath(dir), lockData, 0o444); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(dir, 0o755)
	}
	selections, err := PluginSelections(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(selections) != 1 || selections[0].ID != "github.com/acme/alpha" {
		t.Fatalf("selections = %#v", selections)
	}
	if _, err := os.Lstat(filepath.Join(dir, projectDirectory)); !os.IsNotExist(err) {
		t.Fatalf("read-only PluginSelections created project state: %v", err)
	}
}

func TestPluginSelectionsReadsOneMetadataSnapshot(t *testing.T) {
	dir := t.TempDir()
	snapshot := func(id string) (map[string]any, LockDocument) {
		manifest := map[string]any{"$schema": SchemaURI, "plugins": map[string]any{id: "^1.0.0"}}
		lock := NewLockDocument()
		lock.Plugins[id] = testLockEntry(true, id, map[string]string{})
		return manifest, lock
	}
	manifestA, lockA := snapshot("github.com/acme/alpha")
	manifestB, lockB := snapshot("github.com/acme/beta")
	publish := func(manifest map[string]any, lock LockDocument) error {
		return WithMutation(context.Background(), dir, func(mutation *Mutation) error {
			return mutation.PublishMetadata(manifest, lock)
		})
	}
	if err := publish(manifestA, lockA); err != nil {
		t.Fatal(err)
	}

	// These bounds were calibrated to reproduce the old gap between its two
	// separately locked reads while keeping the regression finite under -race.
	const (
		readers      = 32
		publications = 2000
	)
	done := make(chan struct{})
	errs := make(chan error, readers+1)
	var wait sync.WaitGroup
	for range readers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if _, err := PluginSelections(dir); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	for index := range publications {
		manifest, lock := manifestA, lockA
		if index%2 != 0 {
			manifest, lock = manifestB, lockB
		}
		if err := publish(manifest, lock); err != nil {
			errs <- err
			break
		}
	}
	close(done)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("PluginSelections combined different committed snapshots: %v", err)
		}
	}
}

func jsonEqual(left, right json.RawMessage) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}
