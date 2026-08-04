package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/wagopaths"
)

const Version = 1

type Config struct {
	Version       int             `json:"version"`
	Features      map[string]bool `json:"features"`
	Optimizations map[string]bool `json:"optimizations"`
	Runtime       RuntimeDefaults `json:"runtime"`
}

type RuntimeDefaults struct {
	Parallel               string `json:"parallel"`
	DeferredBoundsChecking bool   `json:"deferredBoundsChecking"`
}

type storedConfig struct {
	Version       int             `json:"version"`
	Features      map[string]bool `json:"features"`
	Optimizations map[string]bool `json:"optimizations"`
	Runtime       struct {
		Parallel               string `json:"parallel"`
		DeferredBoundsChecking *bool  `json:"deferredBoundsChecking"`
	} `json:"runtime"`
}

func Default() Config {
	config := Config{
		Version: Version, Features: map[string]bool{}, Optimizations: map[string]bool{},
		Runtime: RuntimeDefaults{Parallel: "1", DeferredBoundsChecking: true},
	}
	for _, setting := range Features() {
		config.Features[strings.TrimPrefix(setting.Key, "features.")] = setting.Default
	}
	for _, setting := range Optimizations() {
		config.Optimizations[strings.TrimPrefix(setting.Key, "optimizations.")] = setting.Default
	}
	return config
}

func Path() string {
	if path := strings.TrimSpace(os.Getenv("WAGO_CONFIG")); path != "" {
		return path
	}
	return wagopaths.DirsFor("config").ConfigFile("settings.json")
}

func Load() (Config, error) { return LoadFile(Path()) }

func loadGlobalConfigured() (Config, bool, error) {
	path := Path()
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	config, err := LoadFile(path)
	return config, true, err
}

func LoadConfigured() (Config, bool, error) {
	target, err := Open(project.Truthy(project.GlobalEnv), project.Truthy(project.LocalEnv))
	if err != nil {
		return Config{}, false, err
	}
	return target.Config(), target.Configured(), nil
}

func LoadFile(path string) (Config, error) {
	config := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var stored storedConfig
	if err := decoder.Decode(&stored); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if stored.Version != Version {
		return Config{}, fmt.Errorf("unsupported settings version %d (want %d)", stored.Version, Version)
	}
	for name, value := range stored.Features {
		key := "features." + name
		if setting, ok := Lookup(key); !ok || !setting.Available {
			return Config{}, fmt.Errorf("unknown feature setting %q", name)
		} else {
			config.Features[name] = value
		}
	}
	for name, value := range stored.Optimizations {
		key := "optimizations." + name
		if setting, ok := Lookup(key); !ok || !setting.Available {
			return Config{}, fmt.Errorf("unknown optimization setting %q for %s", name, filepath.Base(path))
		} else {
			config.Optimizations[name] = value
		}
	}
	if stored.Runtime.Parallel != "" {
		if err := ValidateParallel(stored.Runtime.Parallel); err != nil {
			return Config{}, err
		}
		config.Runtime.Parallel = canonicalParallel(stored.Runtime.Parallel)
	}
	if stored.Runtime.DeferredBoundsChecking != nil {
		config.Runtime.DeferredBoundsChecking = *stored.Runtime.DeferredBoundsChecking
	}
	return config, nil
}

func Save(config Config) error { return SaveFile(Path(), config) }

func SaveFile(path string, config Config) error {
	if err := Validate(config); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	renameErr := os.Rename(temporaryPath, path)
	if renameErr == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return renameErr
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func Validate(config Config) error {
	if config.Version != Version {
		return fmt.Errorf("settings version must be %d", Version)
	}
	if err := ValidateParallel(config.Runtime.Parallel); err != nil {
		return err
	}
	for name := range config.Features {
		if setting, ok := Lookup("features." + name); !ok || !setting.Available {
			return fmt.Errorf("unknown feature setting %q", name)
		}
	}
	for name := range config.Optimizations {
		if setting, ok := Lookup("optimizations." + name); !ok || !setting.Available {
			return fmt.Errorf("unknown optimization setting %q", name)
		}
	}
	return nil
}

func ValidateParallel(value string) error {
	value = canonicalParallel(value)
	if value == "auto" {
		return nil
	}
	workers, err := strconv.Atoi(value)
	if err != nil || workers < 0 {
		return fmt.Errorf("invalid runtime.parallel %q (want: auto or a non-negative worker count)", value)
	}
	return nil
}

func Set(config *Config, key, value string, experimental bool) error {
	key = CanonicalKey(key)
	switch key {
	case "runtime.parallel":
		if err := ValidateParallel(value); err != nil {
			return err
		}
		config.Runtime.Parallel = canonicalParallel(value)
		return nil
	case "runtime.deferred-bounds-checking":
		enabled, err := ParseBool(value)
		if err != nil {
			return err
		}
		config.Runtime.DeferredBoundsChecking = enabled
		return nil
	}
	setting, ok := Lookup(key)
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	if !setting.Available {
		return fmt.Errorf("%s is not available in this build", key)
	}
	if setting.Experimental && !experimental {
		return fmt.Errorf("%s is experimental; pass --experimental to change it", key)
	}
	enabled, err := ParseBool(value)
	if err != nil {
		return err
	}
	name := key[strings.IndexByte(key, '.')+1:]
	switch {
	case strings.HasPrefix(key, "features."):
		config.Features[name] = enabled
	case strings.HasPrefix(key, "optimizations."):
		config.Optimizations[name] = enabled
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	return nil
}

func Get(config Config, key string) (string, error) {
	key = CanonicalKey(key)
	switch key {
	case "runtime.parallel":
		return config.Runtime.Parallel, nil
	case "runtime.deferred-bounds-checking":
		return strconv.FormatBool(config.Runtime.DeferredBoundsChecking), nil
	}
	setting, ok := Lookup(key)
	if !ok {
		return "", fmt.Errorf("unknown setting %q", key)
	}
	if !setting.Available {
		return "unavailable", nil
	}
	name := key[strings.IndexByte(key, '.')+1:]
	if strings.HasPrefix(key, "features.") {
		return strconv.FormatBool(config.Features[name]), nil
	}
	return strconv.FormatBool(config.Optimizations[name]), nil
}

func Reset(config *Config, key string, experimental bool) error {
	key = CanonicalKey(key)
	defaults := Default()
	value, err := Get(defaults, key)
	if err != nil {
		return err
	}
	return Set(config, key, value, experimental)
}

func canonicalParallel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "serial" {
		return "1"
	}
	return value
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("multiple JSON values")
}
