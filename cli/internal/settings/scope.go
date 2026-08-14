package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/wago-org/wago/cli/internal/project"
)

const (
	ScopeGlobal = "global"
	ScopeLocal  = "local"
	localField  = "settings"
)

type localLayer struct {
	Features      map[string]bool `json:"features,omitempty"`
	Optimizations map[string]bool `json:"optimizations,omitempty"`
	Runtime       *runtimeLayer   `json:"runtime,omitempty"`
}

type runtimeLayer struct {
	Parallel               *string `json:"parallel,omitempty"`
	DeferredBoundsChecking *bool   `json:"deferredBoundsChecking,omitempty"`
}

// Target hides whether settings are stored in the user configuration file or
// as sparse project overrides in wago.json.
type Target struct {
	scope      string
	path       string
	base       Config
	config     Config
	configured bool
	manifest   map[string]any
}

type Override struct {
	Key   string `json:"key"`
	Base  string `json:"base"`
	Value string `json:"value"`
}

func Open(global, local bool) (*Target, error) {
	if global && local {
		return nil, errors.New("choose either --global or --local, not both")
	}
	globalConfig, globalConfigured, err := loadGlobalConfigured()
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(project.Path("."))
	hasManifest := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if local && !hasManifest {
		return nil, fmt.Errorf("no local manifest at %s; run 'wago init' or use --global", project.DisplayPath("."))
	}
	if global || (!local && !hasManifest) {
		return &Target{
			scope: ScopeGlobal, path: Path(), base: Default(), config: cloneConfig(globalConfig), configured: globalConfigured,
		}, nil
	}

	manifest, err := project.Read(".")
	if err != nil {
		return nil, err
	}
	layer, localConfigured, err := decodeLocalLayer(manifest[localField])
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", project.DisplayPath("."), localField, err)
	}
	config := cloneConfig(globalConfig)
	applyLayer(&config, layer)
	return &Target{
		scope: ScopeLocal, path: project.DisplayPath("."), base: cloneConfig(globalConfig), config: config,
		configured: globalConfigured || localConfigured, manifest: manifest,
	}, nil
}

func (target *Target) Scope() string     { return target.scope }
func (target *Target) Path() string      { return target.path }
func (target *Target) Configured() bool  { return target.configured }
func (target *Target) Config() Config    { return cloneConfig(target.config) }
func (target *Target) ResetBase() Config { return cloneConfig(target.base) }

func (target *Target) Set(key, value string, experimental bool) error {
	return Set(&target.config, key, value, experimental)
}

func (target *Target) Get(key string) (string, error) {
	return Get(target.config, key)
}

func (target *Target) Reset(key string, experimental bool) error {
	value, err := Get(target.base, key)
	if err != nil {
		return err
	}
	return Set(&target.config, key, value, experimental)
}

func (target *Target) ResetAll() { target.config = cloneConfig(target.base) }

func (target *Target) Overrides() []Override {
	keys := []string{"runtime.parallel", "runtime.deferred-bounds-checking"}
	for _, setting := range AllBoolean() {
		keys = append(keys, setting.Key)
	}
	sort.Strings(keys)
	overrides := make([]Override, 0)
	for _, key := range keys {
		base, baseErr := Get(target.base, key)
		value, valueErr := Get(target.config, key)
		if baseErr == nil && valueErr == nil && base != value {
			overrides = append(overrides, Override{Key: key, Base: base, Value: value})
		}
	}
	return overrides
}

func (target *Target) Replace(config Config) error {
	if err := Validate(config); err != nil {
		return err
	}
	target.config = cloneConfig(config)
	return nil
}

func (target *Target) Save() error {
	if target.scope == ScopeGlobal {
		return Save(target.config)
	}
	layer := diffLayer(target.config, target.base)
	return project.WithMutation(context.Background(), ".", func(mutation *project.Mutation) error {
		manifest, err := mutation.ReadManifest()
		if err != nil {
			return err
		}
		if layerEmpty(layer) {
			delete(manifest, localField)
		} else {
			manifest[localField] = layer
		}
		project.EnsureMetadata(manifest)
		if err := mutation.PublishManifest(manifest); err != nil {
			return err
		}
		target.manifest = manifest
		return nil
	})
}

func decodeLocalLayer(value any) (localLayer, bool, error) {
	if value == nil {
		return localLayer{}, false, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return localLayer{}, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var layer localLayer
	if err := decoder.Decode(&layer); err != nil {
		return localLayer{}, false, err
	}
	if err := ensureEOF(decoder); err != nil {
		return localLayer{}, false, err
	}
	if err := validateLayer(layer); err != nil {
		return localLayer{}, false, err
	}
	return layer, !layerEmpty(layer), nil
}

func validateLayer(layer localLayer) error {
	if err := ValidateFeatureValues(layer.Features); err != nil {
		return err
	}
	if err := ValidateOptimizationValues(layer.Optimizations); err != nil {
		return err
	}
	if layer.Runtime != nil && layer.Runtime.Parallel != nil {
		if err := ValidateParallel(*layer.Runtime.Parallel); err != nil {
			return err
		}
	}
	return nil
}

func applyLayer(config *Config, layer localLayer) {
	for name, value := range layer.Features {
		setting, _ := Lookup("features." + name)
		setting.SetValue(config, value)
	}
	for name, value := range layer.Optimizations {
		setting, _ := Lookup("optimizations." + name)
		setting.SetValue(config, value)
	}
	if layer.Runtime == nil {
		return
	}
	if layer.Runtime.Parallel != nil {
		config.Runtime.Parallel = canonicalParallel(*layer.Runtime.Parallel)
	}
	if layer.Runtime.DeferredBoundsChecking != nil {
		config.Runtime.DeferredBoundsChecking = *layer.Runtime.DeferredBoundsChecking
	}
}

func diffLayer(config, base Config) localLayer {
	layer := localLayer{}
	for _, setting := range allKnownBoolean() {
		value := setting.Value(config)
		if value != setting.Value(base) {
			name := setting.Name()
			if setting.kind == featureSettingKind {
				if layer.Features == nil {
					layer.Features = map[string]bool{}
				}
				layer.Features[name] = value
			} else {
				if layer.Optimizations == nil {
					layer.Optimizations = map[string]bool{}
				}
				layer.Optimizations[name] = value
			}
		}
	}
	if config.Runtime.Parallel != base.Runtime.Parallel || config.Runtime.DeferredBoundsChecking != base.Runtime.DeferredBoundsChecking {
		layer.Runtime = &runtimeLayer{}
		if config.Runtime.Parallel != base.Runtime.Parallel {
			value := config.Runtime.Parallel
			layer.Runtime.Parallel = &value
		}
		if config.Runtime.DeferredBoundsChecking != base.Runtime.DeferredBoundsChecking {
			value := config.Runtime.DeferredBoundsChecking
			layer.Runtime.DeferredBoundsChecking = &value
		}
	}
	return layer
}

func layerEmpty(layer localLayer) bool {
	return len(layer.Features) == 0 && len(layer.Optimizations) == 0 &&
		(layer.Runtime == nil || (layer.Runtime.Parallel == nil && layer.Runtime.DeferredBoundsChecking == nil))
}

func cloneConfig(config Config) Config {
	clone := config
	clone.Features = make(map[string]bool, len(config.Features))
	for name, value := range config.Features {
		clone.Features[name] = value
	}
	clone.Optimizations = make(map[string]bool, len(config.Optimizations))
	for name, value := range config.Optimizations {
		clone.Optimizations[name] = value
	}
	return clone
}

func Clone(config Config) Config { return cloneConfig(config) }
