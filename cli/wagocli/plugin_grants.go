//go:build !wago_manager

package wagocli

import (
	"encoding/json"
	"sort"
)

// plugin_grants.go reads and writes reviewed capability grants in wago-lock.json.

// pluginGrants returns the capabilities currently granted to plugin id in dir's
// wago-lock.json (nil when the plugin or its capabilities are absent).
func pluginGrants(dir, id string) []string {
	lock, err := readLock(dir)
	if err != nil {
		return nil
	}
	var out []string
	_ = json.Unmarshal(lock.Packages[id].Capabilities, &out)
	return out
}

// setPluginGrants replaces the capabilities granted to plugin id in dir's
// wago-lock.json (sorted for stable diffs), preserving version, required
// capabilities, and opaque plugin config.
func setPluginGrants(dir, id string, caps []string) error {
	lock, err := readLock(dir)
	if err != nil {
		return err
	}
	sorted := append([]string(nil), caps...)
	sort.Strings(sorted)
	raw, err := json.Marshal(sorted)
	if err != nil {
		return err
	}
	entry := lock.Packages[id]
	entry.Capabilities = raw
	lock.Packages[id] = entry
	return writeLock(dir, lock)
}
