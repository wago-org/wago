package wagocli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

// projectFile is the per-project manifest. The plugins to compile into a custom
// wago live under its "dependencies" array, alongside any publish metadata — one
// file, like package.json. Consuming a plugin doesn't require the publish fields;
// a bare {"dependencies": [...]} is enough.
const projectFile = "wago.json"

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

const (
	manifestSchemaURI = "https://wago.sh/schema.json"
	manifestVersion   = "wago/v1"
)

// projectManifestPath returns the wago.json path in dir.
func projectManifestPath(dir string) string { return filepath.Join(dir, projectFile) }

// readProjectMap loads wago.json as a generic map (preserving unknown fields, so
// a publisher's manifest round-trips), or an empty map when the file is absent.
func readProjectMap(dir string) (map[string]any, error) {
	b, err := os.ReadFile(projectManifestPath(dir))
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", projectFile, err)
	}
	return m, nil
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
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%s plugins: %w", projectFile, err)
	}
	var entries []projectPlugin
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("%s plugins must be an array of objects: %w", projectFile, err)
	}
	out := make([]wago.PluginConfig, len(entries))
	seen := map[string]struct{}{}
	for i, entry := range entries {
		entry.Name = strings.TrimSpace(entry.Name)
		if entry.Name == "" {
			return nil, fmt.Errorf("%s plugins[%d] has no name", projectFile, i)
		}
		if _, duplicate := seen[entry.Name]; duplicate {
			return nil, fmt.Errorf("%s plugin %q appears more than once", projectFile, entry.Name)
		}
		seen[entry.Name] = struct{}{}
		caps, budgets, err := parsePluginCapabilities(entry.Name, entry.Capabilities)
		if err != nil {
			return nil, err
		}
		out[i] = wago.PluginConfig{Name: entry.Name, Capabilities: caps, Budgets: budgets, Before: entry.Before, After: entry.After, Config: entry.Config}
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
			return nil, nil, fmt.Errorf("%s plugin %q capabilities: %w", projectFile, name, err)
		}
	} else {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, nil, fmt.Errorf("%s plugin %q capabilities: %w", projectFile, name, err)
		}
		for value, options := range object {
			values = append(values, value)
			if bytes.Equal(options, []byte("true")) {
				continue
			}
			var budget wago.CapabilityBudget
			if err := json.Unmarshal(options, &budget); err != nil {
				return nil, nil, fmt.Errorf("%s plugin %q capability %q: %w", projectFile, name, value, err)
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
			return nil, nil, fmt.Errorf("%s plugin %q has an empty capability", projectFile, name)
		}
		if _, duplicate := capSeen[cap]; duplicate {
			return nil, nil, fmt.Errorf("%s plugin %q repeats capability %q", projectFile, name, cap)
		}
		capSeen[cap], caps[j] = struct{}{}, cap
	}
	if len(budgets) == 0 {
		budgets = nil
	}
	return caps, budgets, nil
}

// writeProjectMap writes wago.json with indented, key-sorted JSON (stable diffs).
func writeProjectMap(dir string, m map[string]any) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(projectManifestPath(dir), append(b, '\n'), 0o644)
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
	if _, ok := m["$schema"]; !ok {
		m["$schema"] = manifestSchemaURI
	}
	if _, ok := m["schema"]; !ok {
		m["schema"] = manifestVersion
	}
	deps := depsFromMap(m)
	for _, d := range deps {
		if d == module {
			return false, nil
		}
	}
	deps = append(deps, module)
	sort.Strings(deps)
	m["dependencies"] = toAnySlice(deps)
	name := module
	plugins, _ := m["plugins"].([]any)
	found := false
	for _, raw := range plugins {
		if entry, ok := raw.(map[string]any); ok && entry["name"] == name {
			found = true
			break
		}
	}
	if !found {
		plugins = append(plugins, map[string]any{"name": name, "capabilities": []any{}})
		m["plugins"] = plugins
	}
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
			if plugins, ok := m["plugins"].([]any); ok {
				filtered := plugins[:0]
				for _, raw := range plugins {
					entry, isEntry := raw.(map[string]any)
					if isEntry && entry["name"] == d {
						continue
					}
					filtered = append(filtered, raw)
				}
				m["plugins"] = filtered
			}
			return true, d, writeProjectMap(dir, m)
		}
	}
	return false, "", nil
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
