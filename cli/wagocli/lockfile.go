package wagocli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// lockfile.go maintains wago-lock.json, a record — next to wago.json — of what
// each installed package required and was granted the last time it was installed.
// It lets `wago pkg add` decide when to (re)run the capability review: on a
// first install, or when a package's required capabilities change.

const lockFileName = "wago-lock.json"

// lockEntry is a package's recorded state.
type lockEntry struct {
	Version              string   `json:"version,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities"`
	GrantedCapabilities  []string `json:"grantedCapabilities"`
}

// lockDoc is the whole wago-lock.json document, keyed by canonical package id.
type lockDoc struct {
	Packages map[string]lockEntry `json:"packages"`
}

func lockPath(dir string) string { return filepath.Join(dir, lockFileName) }

// readLock loads dir's wago-lock.json, returning an empty (non-nil) doc when it's
// absent or unreadable.
func readLock(dir string) lockDoc {
	d := lockDoc{Packages: map[string]lockEntry{}}
	b, err := os.ReadFile(lockPath(dir))
	if err != nil {
		return d
	}
	if json.Unmarshal(b, &d) != nil || d.Packages == nil {
		return lockDoc{Packages: map[string]lockEntry{}}
	}
	return d
}

// writeLock writes dir's wago-lock.json with stable, sorted output.
func writeLock(dir string, d lockDoc) error {
	if d.Packages == nil {
		d.Packages = map[string]lockEntry{}
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath(dir), append(b, '\n'), 0o644)
}

// sameStringSet reports whether a and b contain the same elements (order- and
// duplicate-insensitive).
func sameStringSet(a, b []string) bool {
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
