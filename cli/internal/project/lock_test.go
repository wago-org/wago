package project

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadLock(dir)
	if err != nil || got.Packages == nil || len(got.Packages) != 0 {
		t.Fatalf("ReadLock(empty) = %+v, %v", got, err)
	}

	want := LockDocument{Packages: map[string]LockEntry{
		"wago-org/wasi": {
			Version:              "v0.0.0-x",
			RequiredCapabilities: []string{"host.imports", "host.environment"},
			Capabilities:         json.RawMessage(`["host.imports"]`),
			Config:               json.RawMessage(`{"dir":"/tmp"}`),
		},
	}}
	if err := WriteLock(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err = ReadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotEntry := got.Packages["wago-org/wasi"]
	wantEntry := want.Packages["wago-org/wasi"]
	if gotEntry.Version != wantEntry.Version ||
		!reflect.DeepEqual(gotEntry.RequiredCapabilities, wantEntry.RequiredCapabilities) ||
		!jsonEqual(gotEntry.Capabilities, wantEntry.Capabilities) ||
		!jsonEqual(gotEntry.Config, wantEntry.Config) {
		t.Fatalf("lock round trip:\n got %#v\nwant %#v", got, want)
	}
}

func jsonEqual(left, right json.RawMessage) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}

func TestReadLockRejectsMalformedAuthorityState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(LockPath(dir), []byte(`{"plugins":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLock(dir); err == nil {
		t.Fatal("ReadLock accepted a non-object plugins field")
	}
}
