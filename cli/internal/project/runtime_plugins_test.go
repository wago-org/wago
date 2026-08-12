//go:build !wago_minimal

package project

import (
	"encoding/json"
	"os"
	"reflect"
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

func jsonEqual(left, right json.RawMessage) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}
