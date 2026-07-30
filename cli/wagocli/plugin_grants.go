//go:build !wago_manager

package wagocli

import "sort"

// plugin_grants.go reads and writes the per-plugin capability grants in a
// wago.json. Each entry in the "plugins" array has a GitHub-relative name and a
// "capabilities" array of the privileged APIs that plugin may use at runtime.

// pluginGrants returns the capabilities currently granted to plugin id in dir's
// wago.json (nil when the plugin or its capabilities are absent).
func pluginGrants(dir, id string) []string {
	m, err := readProjectMap(dir)
	if err != nil {
		return nil
	}
	plugins, err := projectPluginMaps(m, dir)
	if err != nil {
		return nil
	}
	entry := projectPluginMap(plugins, id)
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
	plugins, err := projectPluginMaps(m, dir)
	if err != nil {
		return err
	}
	entry := projectPluginMap(plugins, id)
	if entry == nil {
		entry = map[string]any{"name": id}
		plugins = append(plugins, entry)
	}
	sorted := append([]string(nil), caps...)
	sort.Strings(sorted)
	entry["capabilities"] = toAnySlice(sorted)
	m["plugins"] = plugins
	return writeProjectMap(dir, m)
}
