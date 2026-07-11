package wagocli

import (
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

const (
	manifestSchemaURI = "https://raw.githubusercontent.com/wago-org/wago/main/wago.schema.json"
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
	Capabilities []string        `json:"capabilities,omitempty"`
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
		caps := make([]wago.PluginCapability, len(entry.Capabilities))
		capSeen := map[wago.PluginCapability]struct{}{}
		for j, value := range entry.Capabilities {
			cap := wago.PluginCapability(strings.TrimSpace(value))
			if cap == "" {
				return nil, fmt.Errorf("%s plugin %q has an empty capability", projectFile, entry.Name)
			}
			if _, duplicate := capSeen[cap]; duplicate {
				return nil, fmt.Errorf("%s plugin %q repeats capability %q", projectFile, entry.Name, cap)
			}
			capSeen[cap], caps[j] = struct{}{}, cap
		}
		out[i] = wago.PluginConfig{Name: entry.Name, Capabilities: caps, Before: entry.Before, After: entry.After, Config: entry.Config}
	}
	return out, nil
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
	name := deriveName(module)
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

// removeProjectDep removes a dependency matched by full module path or derived
// short name. Returns whether one was removed and the module path it resolved to.
func removeProjectDep(dir, name string) (removed bool, module string, err error) {
	m, err := readProjectMap(dir)
	if err != nil {
		return false, "", err
	}
	deps := depsFromMap(m)
	for i, d := range deps {
		if d == name || deriveName(d) == name {
			short := deriveName(d)
			deps = append(append([]string{}, deps[:i]...), deps[i+1:]...)
			m["dependencies"] = toAnySlice(deps)
			if plugins, ok := m["plugins"].([]any); ok {
				filtered := plugins[:0]
				for _, raw := range plugins {
					entry, isEntry := raw.(map[string]any)
					if isEntry && entry["name"] == short {
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

// deriveName picks a short plugin name from a module path: the last element with a
// leading "wago-"/"wago_" stripped (github.com/acme/wago-redis -> redis).
func deriveName(module string) string {
	base := module
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimPrefix(base, "wago-")
	base = strings.TrimPrefix(base, "wago_")
	return base
}
