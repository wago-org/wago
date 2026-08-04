// Package settings owns Wago's user-configurable runtime defaults.
package settings

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

type BoolSetting struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Default      bool   `json:"default"`
	Experimental bool   `json:"experimental"`
	Available    bool   `json:"available"`
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
	}
}

func optimizationSetting(info wago.OptKnobInfo) BoolSetting {
	return BoolSetting{
		Key: "optimizations." + info.Name, Label: info.Label, Description: info.Desc,
		Default: info.Default, Experimental: info.Experimental, Available: true,
	}
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
