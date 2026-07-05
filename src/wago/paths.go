package wago

import (
	"os"
	"path/filepath"
)

// Dirs resolves wago's on-disk locations. It follows the XDG base-directory
// layout with one deliberate split: config and installed version binaries are
// SHARED across wago versions, while the compiled-module cache is ISOLATED per
// version (keyed by the version string) so a codegen change between versions can
// never serve a stale compiled artifact.
//
// Resolution order: WAGO_HOME (if set) roots everything under one directory;
// otherwise XDG_CONFIG_HOME / XDG_CACHE_HOME / XDG_DATA_HOME, each falling back to
// the usual ~/.config, ~/.cache, ~/.local/share.
type Dirs struct {
	Config   string // shared: user config + plugin manifest
	Data     string // shared: root for installed version binaries
	Versions string // shared: Data/versions/<ver>/
	Cache    string // isolated: per-version compiled-module cache
	Version  string // the version this Dirs was keyed to
}

// DirsFor returns the resolved directories for a given wago version. The version
// keys the compiled-module cache; pass the running binary's version.
func DirsFor(version string) Dirs {
	if version == "" {
		version = "unknown"
	}
	if root := os.Getenv("WAGO_HOME"); root != "" {
		data := filepath.Join(root, "data")
		return Dirs{
			Config:   filepath.Join(root, "config"),
			Data:     data,
			Versions: filepath.Join(data, "versions"),
			Cache:    filepath.Join(root, "cache", version),
			Version:  version,
		}
	}
	data := filepath.Join(xdgDir("XDG_DATA_HOME", ".local", "share"), "wago")
	return Dirs{
		Config:   filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "wago"),
		Data:     data,
		Versions: filepath.Join(data, "versions"),
		Cache:    filepath.Join(xdgDir("XDG_CACHE_HOME", ".cache"), "wago", version),
		Version:  version,
	}
}

// VersionBinary returns the path to an installed wago binary for a version.
func (d Dirs) VersionBinary(version string) string {
	return filepath.Join(d.Versions, version, "wago")
}

// CachePath returns the path to a per-version compiled-module cache entry keyed
// by a caller-chosen key (e.g. a content hash), with a .wago extension.
func (d Dirs) CachePath(key string) string {
	return filepath.Join(d.Cache, key+".wago")
}

// ConfigFile returns the path to a shared config file by name.
func (d Dirs) ConfigFile(name string) string {
	return filepath.Join(d.Config, name)
}

// Ensure creates the config, cache, and versions directories if missing.
func (d Dirs) Ensure() error {
	for _, dir := range []string{d.Config, d.Cache, d.Versions} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// xdgDir returns $env if set, else $HOME joined with the fallback path elements.
func xdgDir(env string, fallback ...string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	return filepath.Join(append([]string{home}, fallback...)...)
}
