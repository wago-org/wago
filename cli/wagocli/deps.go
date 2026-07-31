//go:build !wago_manager

package wagocli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
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

type projectPluginRequirement struct {
	ID         string
	Module     string
	Constraint string
}

var (
	pluginIDPattern         = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._/-]*$`)
	pluginConstraintPattern = regexp.MustCompile(`^(?:\*|[~^]?v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$`)
)

// projectPluginRequirements reads the manifest's plugin version constraints.
// Plugin IDs are GitHub-relative; their module paths are used as build inputs.
func projectPluginRequirements(m map[string]any, dir string) ([]projectPluginRequirement, error) {
	if _, legacy := m["schema"]; legacy {
		return nil, fmt.Errorf("%s: schema was removed; use $schema with %q for editor tooling", projectManifestDisplayPath(dir), manifestSchemaURI)
	}
	if _, legacy := m["dependencies"]; legacy {
		return nil, fmt.Errorf("%s: dependencies was removed; declare versioned entries under plugins", projectManifestDisplayPath(dir))
	}
	raw, ok := m["plugins"]
	if !ok {
		return nil, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: plugins must be an object mapping plugin IDs to version constraints", projectManifestDisplayPath(dir))
	}
	out := make([]projectPluginRequirement, 0, len(values))
	for id, rawConstraint := range values {
		id = strings.TrimSpace(id)
		if !pluginIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%s: plugin ID %q must be GitHub-relative, such as wago-org/wasi", projectManifestDisplayPath(dir), id)
		}
		constraint, ok := rawConstraint.(string)
		if !ok || !pluginConstraintPattern.MatchString(strings.TrimSpace(constraint)) {
			return nil, fmt.Errorf("%s: plugin %q has invalid version constraint %q", projectManifestDisplayPath(dir), id, rawConstraint)
		}
		out = append(out, projectPluginRequirement{
			ID:         id,
			Module:     "github.com/" + id,
			Constraint: strings.TrimSpace(constraint),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// projectPlugins combines wago.json's selected plugins with the resolved
// authority and opaque configuration recorded in wago-lock.json.
func projectPlugins(dir string) ([]wago.PluginConfig, error) {
	m, err := readProjectMap(dir)
	if err != nil {
		return nil, err
	}
	requirements, err := projectPluginRequirements(m, dir)
	if err != nil {
		return nil, err
	}
	lock, err := readLock(dir)
	if err != nil {
		return nil, err
	}
	out := make([]wago.PluginConfig, 0, len(requirements))
	for _, requirement := range requirements {
		entry := lock.Packages[requirement.ID]
		caps, budgets, err := parsePluginCapabilities(requirement.ID, entry.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", projectManifestDisplayPath(dir), err)
		}
		out = append(out, wago.PluginConfig{
			Name:         requirement.ID,
			Capabilities: caps,
			Budgets:      budgets,
			Config:       append(json.RawMessage(nil), entry.Config...),
		})
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

// projectDeps returns module paths derived from the plugin IDs in wago.json.
func projectDeps(dir string) ([]string, error) {
	m, err := readProjectMap(dir)
	if err != nil {
		return nil, err
	}
	requirements, err := projectPluginRequirements(m, dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(requirements))
	for i := range requirements {
		out[i] = requirements[i].Module
	}
	return out, nil
}

// addProjectDep adds or updates a plugin version constraint in wago.json.
func addProjectDep(dir, module, constraint string) (bool, error) {
	if !strings.HasPrefix(module, "github.com/") {
		return false, fmt.Errorf("plugin module %q must be hosted on github.com", module)
	}
	if !pluginConstraintPattern.MatchString(constraint) {
		return false, fmt.Errorf("plugin %q has invalid version constraint %q", strings.TrimPrefix(module, "github.com/"), constraint)
	}
	m, err := readProjectMap(dir)
	if err != nil {
		return false, err
	}
	ensureProjectMetadata(m)
	raw, ok := m["plugins"]
	if !ok {
		raw = map[string]any{}
	}
	plugins, ok := raw.(map[string]any)
	if !ok {
		return false, fmt.Errorf("%s: plugins must be an object mapping plugin IDs to version constraints", projectManifestDisplayPath(dir))
	}
	name := strings.TrimPrefix(module, "github.com/")
	if current, exists := plugins[name]; exists && current == constraint {
		return false, nil
	}
	plugins[name] = constraint
	m["plugins"] = plugins
	return true, writeProjectMap(dir, m)
}

// removeProjectDep removes a dependency by its canonical module path.
func removeProjectDep(dir, name string) (removed bool, module string, err error) {
	m, err := readProjectMap(dir)
	if err != nil {
		return false, "", err
	}
	requirements, err := projectPluginRequirements(m, dir)
	if err != nil {
		return false, "", err
	}
	id := strings.TrimPrefix(name, "github.com/")
	var matchedModule string
	for _, requirement := range requirements {
		if requirement.ID == id || requirement.Module == name {
			matchedModule = requirement.Module
			break
		}
	}
	if matchedModule == "" {
		return false, "", nil
	}
	delete(m["plugins"].(map[string]any), id)
	if err := writeProjectMap(dir, m); err != nil {
		return false, "", err
	}
	lock, lockErr := readLock(dir)
	if lockErr != nil {
		return false, "", lockErr
	}
	delete(lock.Packages, id)
	if err := writeLock(dir, lock); err != nil {
		return false, "", err
	}
	return true, matchedModule, nil
}

// deriveName is retained for registry display plumbing; plugin identities are
// canonical module paths and are never shortened.
func deriveName(module string) string { return module }
