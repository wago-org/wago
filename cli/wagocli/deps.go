//go:build !wago_manager

package wagocli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

// normalizeModuleRef canonicalizes a user-typed package reference to a full
// module path: a bare "owner/repo" (whose first segment has no dot, so it's not a
// host) is assumed to live on github.com, matching how the registry and wago.json
// store plugin identities. Any "@version" suffix is preserved. This lets commands
// accept "wago-org/wasi" and "github.com/wago-org/wasi" interchangeably.
func normalizeModuleRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref
	}
	path, ver := ref, ""
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		path, ver = ref[:i], ref[i:]
	}
	// Only "owner/repo" shapes get a host: a bare token (no slash, e.g. "wasi") is
	// a short name, left untouched. A dot in the first segment means a host is
	// already present (github.com, gitlab.com); otherwise default to github.com.
	if i := strings.IndexByte(path, '/'); i > 0 {
		if first := path[:i]; !strings.Contains(first, ".") {
			path = "github.com/" + path
		}
	}
	return path + ver
}

type projectPlugin struct {
	Name         string          `json:"name"`
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
	Before       []string        `json:"before,omitempty"`
	After        []string        `json:"after,omitempty"`
	Config       json.RawMessage `json:"config,omitempty"`
}

// projectPlugins reads the manifest's enabled plugin plan. Dependencies decide
// what is compiled into the custom binary; plugins decide what is activated and
// exactly which privileged Wago APIs each one may exercise.
func projectPlugins(dir string) ([]wago.PluginConfig, error) {
	m, err := readProjectMap(dir)
	if err != nil {
		return nil, err
	}
	raw, ok := m["plugins"]
	if !ok {
		return nil, nil
	}
	if _, ok := raw.([]any); !ok {
		return nil, fmt.Errorf("%s: plugins must be an array of plugin objects", projectManifestDisplayPath(dir))
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: plugins: %w", projectManifestDisplayPath(dir), err)
	}
	var entries []projectPlugin
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("%s: invalid plugins array: %w", projectManifestDisplayPath(dir), err)
	}
	out := make([]wago.PluginConfig, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			return nil, fmt.Errorf("%s: plugin name is empty", projectManifestDisplayPath(dir))
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("%s: plugin %q is listed more than once", projectManifestDisplayPath(dir), name)
		}
		seen[name] = struct{}{}
		caps, budgets, err := parsePluginCapabilities(name, entry.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", projectManifestDisplayPath(dir), err)
		}
		out = append(out, wago.PluginConfig{Name: name, Capabilities: caps, Budgets: budgets, Before: entry.Before, After: entry.After, Config: entry.Config})
	}
	return out, nil
}

func parsePluginCapabilities(name string, raw json.RawMessage) ([]wago.PluginCapability, map[wago.PluginCapability]wago.CapabilityBudget, error) {
	if len(raw) == 0 {
		raw = []byte("[]")
	}
	var values []string
	budgets := map[wago.PluginCapability]wago.CapabilityBudget{}
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, nil, fmt.Errorf("plugin %q capabilities: %w", name, err)
		}
	} else {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, nil, fmt.Errorf("plugin %q capabilities: %w", name, err)
		}
		for value, options := range object {
			values = append(values, value)
			if bytes.Equal(options, []byte("true")) {
				continue
			}
			var budget wago.CapabilityBudget
			if err := json.Unmarshal(options, &budget); err != nil {
				return nil, nil, fmt.Errorf("plugin %q capability %q: %w", name, value, err)
			}
			budgets[wago.PluginCapability(value)] = budget
		}
		sort.Strings(values)
	}
	caps := make([]wago.PluginCapability, len(values))
	capSeen := map[wago.PluginCapability]struct{}{}
	for j, value := range values {
		cap := wago.PluginCapability(strings.TrimSpace(value))
		if cap == "" {
			return nil, nil, fmt.Errorf("plugin %q has an empty capability", name)
		}
		if _, duplicate := capSeen[cap]; duplicate {
			return nil, nil, fmt.Errorf("plugin %q repeats capability %q", name, cap)
		}
		capSeen[cap], caps[j] = struct{}{}, cap
	}
	if len(budgets) == 0 {
		budgets = nil
	}
	return caps, budgets, nil
}

// depsFromMap extracts the module paths under "dependencies".
func depsFromMap(m map[string]any) []string {
	raw, _ := m["dependencies"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// projectDeps returns the module paths declared under "dependencies" in dir's
// wago.json (empty when there is no file or no dependencies).
func projectDeps(dir string) ([]string, error) {
	m, err := readProjectMap(dir)
	if err != nil {
		return nil, err
	}
	return depsFromMap(m), nil
}

// addProjectDep adds module to wago.json's dependencies (idempotent), creating the
// file if absent. Returns whether it was newly added.
func addProjectDep(dir, module string) (bool, error) {
	m, err := readProjectMap(dir)
	if err != nil {
		return false, err
	}
	ensureProjectMetadata(m)
	deps := depsFromMap(m)
	for _, d := range deps {
		if d == module {
			return false, nil
		}
	}
	deps = append(deps, module)
	sort.Strings(deps)
	m["dependencies"] = toAnySlice(deps)
	name := strings.TrimPrefix(module, "github.com/")
	plugins, err := projectPluginMaps(m, dir)
	if err != nil {
		return false, err
	}
	if projectPluginMap(plugins, name) == nil {
		plugins = append(plugins, map[string]any{"name": name, "capabilities": []any{}})
	}
	m["plugins"] = plugins
	return true, writeProjectMap(dir, m)
}

// removeProjectDep removes a dependency by its canonical module path.
func removeProjectDep(dir, name string) (removed bool, module string, err error) {
	m, err := readProjectMap(dir)
	if err != nil {
		return false, "", err
	}
	deps := depsFromMap(m)
	for i, d := range deps {
		if d == name {
			deps = append(append([]string{}, deps[:i]...), deps[i+1:]...)
			m["dependencies"] = toAnySlice(deps)
			plugins, pluginErr := projectPluginMaps(m, dir)
			if pluginErr != nil {
				return false, "", pluginErr
			}
			id := strings.TrimPrefix(d, "github.com/")
			for j, entry := range plugins {
				if entry["name"] == id {
					plugins = append(plugins[:j], plugins[j+1:]...)
					break
				}
			}
			m["plugins"] = plugins
			return true, d, writeProjectMap(dir, m)
		}
	}
	return false, "", nil
}

func projectPluginMaps(m map[string]any, dir string) ([]map[string]any, error) {
	raw, ok := m["plugins"]
	if !ok {
		return []map[string]any{}, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: plugins must be an array of plugin objects", projectManifestDisplayPath(dir))
	}
	entries := make([]map[string]any, 0, len(values))
	for i, value := range values {
		entry, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: plugins[%d] must be an object", projectManifestDisplayPath(dir), i)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func projectPluginMap(entries []map[string]any, id string) map[string]any {
	for _, entry := range entries {
		if entry["name"] == id {
			return entry
		}
	}
	return nil
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// deriveName is retained for registry display plumbing; plugin identities are
// canonical module paths and are never shortened.
func deriveName(module string) string { return module }
