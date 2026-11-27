// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package settings owns Wago's user-configurable runtime defaults.
//
//go:generate go run ./cmd/genschema -schema ../../../schema.json
package settings

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/buildfeatures"
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
	result := make([]BoolSetting, 0, len(infos))
	for _, info := range infos {
		if info.Desc != "" {
			result = append(result, optimizationSetting(info))
		}
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
	index, ok := knownIndex().byKey[CanonicalKey(key)]
	if !ok {
		return BoolSetting{}, false
	}
	return allKnownBoolean()[index], true
}

func CanonicalKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "_", "-")
	if strings.Contains(key, ".") {
		return key
	}
	if match, ok := knownIndex().byShort[key]; ok && match != "" {
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

// allKnownBoolean returns every feature and optimization setting. The catalog
// is fixed once the process starts, and Lookup and CanonicalKey consult it for
// every configured value, so it is built once instead of on each call. The
// result is shared and clipped: callers must not modify its elements.
func allKnownBoolean() []BoolSetting {
	items := knownBoolean()
	return items[:len(items):len(items)]
}

var knownBoolean = sync.OnceValue(func() []BoolSetting {
	return append(allFeatures(), OptimizationCatalog()...)
})

type settingIndex struct {
	byKey map[string]int
	// byShort maps a name without its section prefix to its full key, or to ""
	// when the name is ambiguous across sections.
	byShort map[string]string
}

var knownIndex = sync.OnceValue(func() settingIndex {
	items := knownBoolean()
	index := settingIndex{byKey: make(map[string]int, len(items)), byShort: make(map[string]string, len(items))}
	for position, setting := range items {
		if _, seen := index.byKey[setting.Key]; !seen {
			index.byKey[setting.Key] = position
		}
		for _, short := range []string{strings.TrimPrefix(setting.Key, "features."), strings.TrimPrefix(setting.Key, "optimizations.")} {
			if short == setting.Key {
				continue
			}
			if previous, seen := index.byShort[short]; seen && previous != setting.Key {
				index.byShort[short] = ""
			} else if !seen {
				index.byShort[short] = setting.Key
			}
		}
	}
	return index
})

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
	seen := make(map[string]string, len(values))
	for name, enabled := range values {
		setting, ok := Lookup(prefix + name)
		if !ok || setting.kind != kind {
			return fmt.Errorf("unknown %s setting %q", label, name)
		}
		if err := recordCanonicalSetting(seen, setting.Key, name, label); err != nil {
			return err
		}
		if enabled && !setting.Available && !buildOmittedFeature(setting) {
			return fmt.Errorf("%s setting %q is unavailable", label, name)
		}
	}
	return nil
}

func recordCanonicalSetting(seen map[string]string, canonical, name, label string) error {
	previous, duplicate := seen[canonical]
	if !duplicate {
		seen[canonical] = name
		return nil
	}
	if name < previous {
		previous, name = name, previous
	}
	return fmt.Errorf("duplicate %s settings %q and %q", label, previous, name)
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

// An installed feature-trimmed runtime can share its settings file with the
// full manager or another runtime build. Retain their stored defaults.
func buildOmittedFeature(setting BoolSetting) bool {
	if buildfeatures.Custom && setting.kind == featureSettingKind {
		feature, _ := wago.FeatureInfoByName(setting.name)
		return feature.Feature&wago.CoreFeatures(buildfeatures.Enabled) == 0
	}
	return false
}
