// Package wagopaths resolves Wago's manager, runner, configuration, and cache
// paths without importing the runtime.
package wagopaths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Profile string
type Build string

const (
	ProfileStandard Profile = "standard"
	ProfileMinimal  Profile = "minimal"

	BuildNormal Build = "normal"
	BuildTiny   Build = "tiny"
)

var Profiles = []Profile{ProfileStandard, ProfileMinimal}
var Builds = []Build{BuildNormal, BuildTiny}

func ParseProfile(value string) (Profile, error) {
	profile := Profile(strings.ToLower(strings.TrimSpace(value)))
	for _, candidate := range Profiles {
		if profile == candidate {
			return profile, nil
		}
	}
	return "", fmt.Errorf("unknown profile %q (want: standard or minimal)", value)
}

func (p Profile) Description() string {
	switch p {
	case ProfileStandard:
		return "Everything"
	case ProfileMinimal:
		return "Run only"
	default:
		return ""
	}
}

func ParseBuild(value string) (Build, error) {
	build := Build(strings.ToLower(strings.TrimSpace(value)))
	for _, candidate := range Builds {
		if build == candidate {
			return build, nil
		}
	}
	return "", fmt.Errorf("unknown build %q (want: normal or tiny)", value)
}

func (b Build) Description() string {
	switch b {
	case BuildNormal:
		return "Standard Go · fastest runtime"
	case BuildTiny:
		return "TinyGo · smaller binary"
	default:
		return ""
	}
}

type Dirs struct {
	Config   string
	Data     string
	Versions string
	Cache    string
	Version  string
}

func DirsFor(version string) Dirs {
	return dirsFor(version, runtime.GOOS)
}

func dirsFor(version, goos string) Dirs {
	if version == "" {
		version = "unknown"
	}
	if root := os.Getenv("WAGO_HOME"); root != "" {
		data := filepath.Join(root, "data")
		return Dirs{
			Config: filepath.Join(root, "config"), Data: data,
			Versions: filepath.Join(data, "versions"),
			Cache:    filepath.Join(root, "cache", version), Version: version,
		}
	}
	if goos == "darwin" {
		root := filepath.Join(homeDir(), ".wago")
		return Dirs{
			Config: filepath.Join(root, "config"), Data: root,
			Versions: filepath.Join(root, "versions"),
			Cache:    filepath.Join(root, "cache", version), Version: version,
		}
	}
	data := filepath.Join(xdgDir("XDG_DATA_HOME", ".local", "share"), "wago")
	return Dirs{
		Config: filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "wago"), Data: data,
		Versions: filepath.Join(data, "versions"),
		Cache:    filepath.Join(xdgDir("XDG_CACHE_HOME", ".cache"), "wago", version), Version: version,
	}
}

// VersionBinary is the legacy monolithic-binary location.
func (d Dirs) VersionBinary(version string) string {
	return filepath.Join(d.Versions, version, "wago")
}

// RuntimeBinary is the current profile/build-specific executable location.
func (d Dirs) RuntimeBinary(version, profile, build string) string {
	name := "wago-runtime"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(d.Versions, version, profile, build, name)
}

// RunnerBinary is the pre-build-variant executable location. It remains
// readable as the Normal build so existing installations keep working.
func (d Dirs) RunnerBinary(version, profile string) string {
	name := "wago-runtime"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(d.Versions, version, profile, name)
}

// LegacyRunnerBinary is the pre-runtime-rename executable location. It remains
// readable so existing installations keep working after a manager upgrade.
func (d Dirs) LegacyRunnerBinary(version, profile string) string {
	name := "wago-runner"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(d.Versions, version, profile, name)
}

func (d Dirs) CachePath(key string) string   { return filepath.Join(d.Cache, key+".wago") }
func (d Dirs) ConfigFile(name string) string { return filepath.Join(d.Config, name) }

func (d Dirs) Ensure() error {
	for _, dir := range []string{d.Config, d.Cache, d.Versions} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func xdgDir(env string, fallback ...string) string {
	if value := os.Getenv(env); value != "" {
		return value
	}
	return filepath.Join(append([]string{homeDir()}, fallback...)...)
}

func homeDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	return home
}
