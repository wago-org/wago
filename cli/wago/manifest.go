package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// manifestName is the per-project plugin manifest file. It is plain JSON — no
// database — because it is a small, git-committable, human-editable list of which
// plugins a custom wago binary should be built with. It is read whole and written
// whole; there are no concurrent writers (it is edited by dev-time CLI actions).
const manifestName = "wago-plugins.json"

const manifestVersion = 1

// Manifest is the declarative set of plugins for a custom wago build.
type Manifest struct {
	Version int           `json:"version"`
	Plugins []PluginEntry `json:"plugins"`
}

// PluginEntry is one declared plugin. Module is "builtin" for a plugin already
// compiled into wago, or a Go module path (e.g. github.com/acme/wago-redis) for a
// third-party one. Version is the module version (empty = latest) and is unused
// for builtins. Enabled plugins are included by `wago plugin build`.
type PluginEntry struct {
	Name    string `json:"name"`
	Module  string `json:"module"`
	Version string `json:"version,omitempty"`
	Enabled bool   `json:"enabled"`
}

// Builtin reports whether the entry refers to a compiled-in plugin.
func (e PluginEntry) Builtin() bool { return e.Module == "" || e.Module == "builtin" }

// manifestPath returns the manifest file path (WAGO_PLUGIN_MANIFEST override, else
// wago-plugins.json in the working directory).
func manifestPath() string {
	if p := os.Getenv("WAGO_PLUGIN_MANIFEST"); p != "" {
		return p
	}
	return manifestName
}

// loadManifest reads the manifest, returning an empty one if the file is absent.
func loadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Manifest{Version: manifestVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.Version == 0 {
		m.Version = manifestVersion
	}
	return &m, nil
}

// save writes the manifest as indented JSON, sorted by name for stable diffs.
func (m *Manifest) save(path string) error {
	sort.Slice(m.Plugins, func(i, j int) bool { return m.Plugins[i].Name < m.Plugins[j].Name })
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// find returns the index of the entry named name, or -1.
func (m *Manifest) find(name string) int {
	for i := range m.Plugins {
		if m.Plugins[i].Name == name {
			return i
		}
	}
	return -1
}

// add inserts or updates an entry, returning whether it was newly added.
func (m *Manifest) add(e PluginEntry) bool {
	if i := m.find(e.Name); i >= 0 {
		m.Plugins[i] = e
		return false
	}
	m.Plugins = append(m.Plugins, e)
	return true
}

// remove deletes the entry named name, returning whether it existed.
func (m *Manifest) remove(name string) bool {
	i := m.find(name)
	if i < 0 {
		return false
	}
	m.Plugins = append(m.Plugins[:i], m.Plugins[i+1:]...)
	return true
}

// ---- CLI ----------------------------------------------------------------

// deriveName picks a plugin name from a module path: the last path element with a
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

// pluginManifestAdd records a plugin in the manifest. modOrName is a builtin
// plugin name (no slash) or a Go module path; name/version override the derived
// name and pin a version.
func pluginManifestAdd(modOrName, name, version string) {
	path := manifestPath()
	m, err := loadManifest(path)
	if err != nil {
		fatal("plugin add: %v", err)
	}
	e := PluginEntry{Enabled: true, Version: version}
	if strings.Contains(modOrName, "/") || strings.Contains(modOrName, ".") {
		e.Module = modOrName
		e.Name = name
		if e.Name == "" {
			e.Name = deriveName(modOrName)
		}
	} else {
		// A bare token: a builtin plugin name.
		e.Module = "builtin"
		e.Name = modOrName
		if name != "" {
			e.Name = name
		}
	}
	if e.Name == "" {
		fatal("plugin add: could not derive a plugin name from %q; pass --name", modOrName)
	}
	newly := m.add(e)
	if err := m.save(path); err != nil {
		fatal("plugin add: %v", err)
	}
	verb := "updated"
	if newly {
		verb = "added"
	}
	fmt.Printf("%s %s (%s) in %s\n", verb, cyan(e.Name), e.Module, path)
}

func pluginManifestRemove(name string) {
	path := manifestPath()
	m, err := loadManifest(path)
	if err != nil {
		fatal("plugin remove: %v", err)
	}
	if !m.remove(name) {
		fatal("plugin remove: %q is not in %s", name, path)
	}
	if err := m.save(path); err != nil {
		fatal("plugin remove: %v", err)
	}
	fmt.Printf("removed %s from %s\n", name, path)
}

func pluginManifestShow() {
	path := manifestPath()
	m, err := loadManifest(path)
	if err != nil {
		fatal("plugin manifest: %v", err)
	}
	if len(m.Plugins) == 0 {
		fmt.Printf("%s\n", dim("no plugins declared in "+path+"; add one: wago pkg install <module>"))
		return
	}
	fmt.Printf("%s %s\n", bold("manifest:"), dim(path))
	for _, e := range m.Plugins {
		state := cyan("enabled")
		if !e.Enabled {
			state = dim("disabled")
		}
		src := e.Module
		if e.Builtin() {
			src = dim("builtin")
		} else if e.Version != "" {
			src = e.Module + "@" + e.Version
		}
		fmt.Printf("  %s  %s  %s\n", e.Name, src, state)
	}
}
