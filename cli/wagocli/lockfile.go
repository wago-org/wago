//go:build !wago_manager

package wagocli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// lockfile.go maintains wago-lock.json, the resolved plugin state next to
// wago.json. Version constraints remain user intent in the manifest; exact
// versions, authority grants, and opaque plugin config live here.

const lockFileName = "wago-lock.json"

// lockEntry is a package's recorded state.
type lockEntry struct {
	Version              string          `json:"version,omitempty"`
	RequiredCapabilities []string        `json:"requiredCapabilities"`
	Capabilities         json.RawMessage `json:"capabilities"`
	Config               json.RawMessage `json:"config,omitempty"`
}

// lockDoc is the whole wago-lock.json document, keyed by canonical package id.
type lockDoc struct {
	Packages map[string]lockEntry `json:"plugins"`
}

func lockPath(dir string) string { return filepath.Join(dir, lockFileName) }

// readLock loads dir's wago-lock.json. Absence means no resolved state; malformed
// authority-bearing state is rejected rather than silently disabling grants.
func readLock(dir string) (lockDoc, error) {
	d := lockDoc{Packages: map[string]lockEntry{}}
	b, err := os.ReadFile(lockPath(dir))
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return lockDoc{}, fmt.Errorf("%s: %w", displayPath(lockPath(dir)), err)
	}
	if d.Packages == nil {
		d.Packages = map[string]lockEntry{}
	}
	return d, nil
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
