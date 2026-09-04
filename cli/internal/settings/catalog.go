// Package settings owns Wago's user-configurable runtime defaults.
//
//go:generate go run ./cmd/genschema -schema ../../../schema.json
package settings

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/project"
)

type BoolSetting struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Default      bool   `json:"default"`
	Experimental bool   `json:"experimental"`
	Available    bool   `json:"available"`
	kind         boolSettingKind
	name         string
}

type boolSettingKind uint8

const (
	featureSettingKind boolSettingKind = iota + 1
	optimizationSettingKind
)

// Name returns the setting name without its canonical section prefix.
func (setting BoolSetting) Name() string { return setting.name }

// Value reads this setting from config without exposing its storage section.
func (setting BoolSetting) Value(config Config) bool {
	if setting.kind == featureSettingKind {
		return config.Features[setting.name]
	}
	return config.Optimizations[setting.name]
}

// SetValue writes this setting to config without exposing its storage section.
func (setting BoolSetting) SetValue(config *Config, enabled bool) {
	if setting.kind == featureSettingKind {
		config.Features[setting.name] = enabled
		return
	}
	config.Optimizations[setting.name] = enabled
}

func Features() []BoolSetting {
	var result []BoolSetting
	for _, feature := range wago.FeatureInfos() {
		if !feature.Experimental {
			result = append(result, featureSetting(feature))
		}
	}
	return result
}

func Optimizations() []BoolSetting { return OptimizationsForArch(runtime.GOARCH) }

func OptimizationsForArch(arch string) []BoolSetting {
	infos := wago.OptimizationInfosForArch(arch)
	result := make([]BoolSetting, len(infos))
	for index, info := range infos {
		result[index] = optimizationSetting(info)
	}
	return result
}

// OptimizationCatalog returns every registered optimization once. It is used
// to build the target-independent flag surface for standalone compilation.
func OptimizationCatalog() []BoolSetting {
	infos := wago.OptimizationInfos()
	result := make([]BoolSetting, len(infos))
	for index, info := range infos {
		result[index] = optimizationSetting(info)
	}
	return result
}

func Experimental() []BoolSetting {
	var result []BoolSetting
	for _, feature := range wago.FeatureInfos() {
		if feature.Experimental {
			result = append(result, featureSetting(feature))
		}
	}
	for _, optimization := range Optimizations() {
		if optimization.Experimental {
			result = append(result, optimization)
		}
	}
	return result
}

func StableOptimizations() []BoolSetting {
	var result []BoolSetting
	for _, setting := range Optimizations() {
		if !setting.Experimental {
			result = append(result, setting)
		}
	}
	return result
}

func AllBoolean() []BoolSetting {
	items := append(allFeatures(), Optimizations()...)
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}

func Lookup(key string) (BoolSetting, bool) {
	key = CanonicalKey(key)
	for _, setting := range allKnownBoolean() {
		if setting.Key == key {
			return setting, true
		}
	}
	return BoolSetting{}, false
}

func CanonicalKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "_", "-")
	if strings.Contains(key, ".") {
		return key
	}
	var match string
	for _, setting := range allKnownBoolean() {
		if strings.TrimPrefix(setting.Key, "features.") == key || strings.TrimPrefix(setting.Key, "optimizations.") == key {
			if match != "" && match != setting.Key {
				return key
			}
			match = setting.Key
		}
	}
	if match != "" {
		return match
	}
	return key
}

func allFeatures() []BoolSetting {
	infos := wago.FeatureInfos()
	result := make([]BoolSetting, len(infos))
	for index, info := range infos {
		result[index] = featureSetting(info)
	}
	return result
}

func allKnownBoolean() []BoolSetting {
	items := append(allFeatures(), OptimizationCatalog()...)
	return items
}

func featureSetting(info wago.FeatureInfo) BoolSetting {
	return BoolSetting{
		Key: "features." + info.Name, Label: info.Label, Description: info.Description,
		Default: info.Default, Experimental: info.Experimental, Available: info.Available,
		kind: featureSettingKind, name: info.Name,
	}
}

func optimizationSetting(info wago.OptKnobInfo) BoolSetting {
	return BoolSetting{
		Key: "optimizations." + info.Name, Label: info.Label, Description: info.Desc,
		Default: info.Default, Experimental: info.Experimental, Available: true,
		kind: optimizationSettingKind, name: info.Name,
	}
}

func validateValues(kind boolSettingKind, values map[string]bool) error {
	prefix := "features."
	label := "feature"
	if kind == optimizationSettingKind {
		prefix = "optimizations."
		label = "optimization"
	}
	for name, enabled := range values {
		setting, ok := Lookup(prefix + name)
		if !ok || setting.kind != kind {
			return fmt.Errorf("unknown %s setting %q", label, name)
		}
		if enabled && !setting.Available {
			return fmt.Errorf("%s setting %q is unavailable", label, name)
		}
	}
	return nil
}

func ValidateFeatureValues(values map[string]bool) error {
	return validateValues(featureSettingKind, values)
}

func ValidateOptimizationValues(values map[string]bool) error {
	return validateValues(optimizationSettingKind, values)
}

// SchemaNames returns registered setting names grouped by manifest section.
func SchemaNames() map[string][]string {
	result := map[string][]string{"features": {}, "optimizations": {}}
	for _, setting := range allKnownBoolean() {
		section := "optimizations"
		if setting.kind == featureSettingKind {
			section = "features"
		}
		result[section] = append(result[section], setting.name)
	}
	// The URI remains v1, so its editor schema must continue to accept retired
	// optimization properties even though they no longer appear in the active
	// runtime catalog.
	result["optimizations"] = append(result["optimizations"], project.RetiredOptimizationNames()...)
	for section := range result {
		sort.Strings(result[section])
	}
	return result
}

func ParseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true, nil
	case "0", "false", "no", "off", "disable", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q (want: on or off)", value)
	}
}
