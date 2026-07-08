package wagocli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// projectFile is the per-project manifest. The plugins to compile into a custom
// wago live under its "dependencies" array, alongside any publish metadata — one
// file, like package.json. Consuming a plugin doesn't require the publish fields;
// a bare {"dependencies": [...]} is enough.
const projectFile = "wago.json"

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
	deps := depsFromMap(m)
	for _, d := range deps {
		if d == module {
			return false, nil
		}
	}
	deps = append(deps, module)
	sort.Strings(deps)
	m["dependencies"] = toAnySlice(deps)
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
			deps = append(append([]string{}, deps[:i]...), deps[i+1:]...)
			m["dependencies"] = toAnySlice(deps)
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
