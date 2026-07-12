package wagocli

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Absent lockfile reads as empty (non-nil).
	if got := readLock(dir); got.Packages == nil || len(got.Packages) != 0 {
		t.Fatalf("empty read: %+v", got)
	}

	d := lockDoc{Packages: map[string]lockEntry{
		"wago-org/wasi": {
			Version:              "v0.0.0-x",
			RequiredCapabilities: []string{"host.imports", "host.environment"},
			GrantedCapabilities:  []string{"host.imports"},
		},
	}}
	if err := writeLock(dir, d); err != nil {
		t.Fatal(err)
	}
	if _, err := filepath.Abs(lockPath(dir)); err != nil {
		t.Fatal(err)
	}
	got := readLock(dir)
	if !reflect.DeepEqual(got, d) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, d)
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
