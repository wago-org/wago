package wagocli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Absent lockfile reads as empty (non-nil).
	if got, err := readLock(dir); err != nil || got.Packages == nil || len(got.Packages) != 0 {
		t.Fatalf("empty read: %+v", got)
	}

	d := lockDoc{Packages: map[string]lockEntry{
		"wago-org/wasi": {
			Version:              "v0.0.0-x",
			RequiredCapabilities: []string{"host.imports", "host.environment"},
			Capabilities:         json.RawMessage(`["host.imports"]`),
			Config:               json.RawMessage(`{"dir":"/tmp"}`),
		},
	}}
	if err := writeLock(dir, d); err != nil {
		t.Fatal(err)
	}
	if _, err := filepath.Abs(lockPath(dir)); err != nil {
		t.Fatal(err)
	}
	got, err := readLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Packages["wago-org/wasi"].Version != d.Packages["wago-org/wasi"].Version ||
		!reflect.DeepEqual(got.Packages["wago-org/wasi"].RequiredCapabilities, d.Packages["wago-org/wasi"].RequiredCapabilities) ||
		!reflect.DeepEqual(decodeJSON(t, got.Packages["wago-org/wasi"].Capabilities), decodeJSON(t, d.Packages["wago-org/wasi"].Capabilities)) ||
		!reflect.DeepEqual(decodeJSON(t, got.Packages["wago-org/wasi"].Config), decodeJSON(t, d.Packages["wago-org/wasi"].Config)) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, d)
	}
}

func TestReadLockRejectsMalformedAuthorityState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(lockPath(dir), []byte(`{"plugins":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLock(dir); err == nil {
		t.Fatal("readLock accepted a non-object plugins field")
	}
}

func TestSameStringSet(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{[]string{"a", "b"}, []string{"b", "a"}, true}, // order-insensitive
		{[]string{"a"}, []string{"a", "b"}, false},
		{nil, nil, true},
		{[]string{"a"}, nil, false},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
	}
	for _, tc := range cases {
		if got := sameStringSet(tc.a, tc.b); got != tc.want {
			t.Errorf("sameStringSet(%v,%v)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
