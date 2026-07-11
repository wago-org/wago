package wagocli

import "sort"

// plugin_grants.go reads and writes the per-plugin capability grants in a
// wago.json — the "plugins" object keyed by GitHub-relative ID (see #246), where
// each entry carries a "capabilities" array of the privileged APIs that plugin is
// allowed to use at runtime.

// pluginGrants returns the capabilities currently granted to plugin id in dir's
// wago.json (nil when the plugin or its capabilities are absent).
func pluginGrants(dir, id string) []string {
	m, err := readProjectMap(dir)
	if err != nil {
		return nil
	}
	plugins, _ := m["plugins"].(map[string]any)
	entry, _ := plugins[id].(map[string]any)
	raw, _ := entry["capabilities"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// setPluginGrants replaces the capabilities granted to plugin id in dir's
// wago.json (sorted for stable diffs), creating the plugin entry if needed while
// preserving any other fields on it (before/after/config).
func setPluginGrants(dir, id string, caps []string) error {
	m, err := readProjectMap(dir)
	if err != nil {
		return err
	}
	plugins, _ := m["plugins"].(map[string]any)
	if plugins == nil {
		plugins = map[string]any{}
	}
	entry, _ := plugins[id].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}
	sorted := append([]string(nil), caps...)
	sort.Strings(sorted)
	entry["capabilities"] = toAnySlice(sorted)
	plugins[id] = entry
	m["plugins"] = plugins
	return writeProjectMap(dir, m)
}
