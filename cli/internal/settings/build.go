// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package settings

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/buildfeatures"
)

// BuildProfile is a Core release with explicit feature overrides. It is
// independent of the features enabled by default at runtime.
type BuildProfile struct {
	Core     string          `json:"core,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

func (p *BuildProfile) core() string {
	if p == nil {
		return ""
	}
	return p.Core
}

func (p *BuildProfile) features() map[string]bool {
	if p == nil {
		return nil
	}
	return p.Features
}

func (p *BuildProfile) empty() bool { return p.core() == "" && len(p.features()) == 0 }

func (p *BuildProfile) clone() *BuildProfile {
	if p == nil {
		return nil
	}
	return &BuildProfile{Core: p.Core, Features: maps.Clone(p.Features)}
}

func sameBuildProfile(left, right *BuildProfile) bool {
	return left.core() == right.core() && maps.Equal(left.features(), right.features())
}

func mergeBuildProfile(base, override *BuildProfile) *BuildProfile {
	profile := &BuildProfile{Core: base.core(), Features: make(map[string]bool)}
	if override.Core != "" {
		profile.Core = override.Core
	}
	maps.Copy(profile.Features, base.features())
	maps.Copy(profile.Features, override.Features)
	return profile
}

func diffBuildProfile(profile, base *BuildProfile) *BuildProfile {
	diff := &BuildProfile{Features: make(map[string]bool)}
	if profile.core() != base.core() {
		diff.Core = profile.core()
	}
	for name, enabled := range profile.features() {
		if inherited, ok := base.features()[name]; !ok || inherited != enabled {
			diff.Features[name] = enabled
		}
	}
	if diff.empty() {
		return nil
	}
	return diff
}

const relaxedSIMD = wago.CoreFeatures(buildfeatures.RelaxedSIMDBit)

var buildFeatures = sync.OnceValue(func() []wago.FeatureInfo {
	return append(wago.FeatureInfos(), wago.FeatureInfo{Feature: relaxedSIMD, Name: "relaxed-simd"})
})

func lookupBuildFeature(name string) (wago.CoreFeatures, bool) {
	for _, feature := range buildFeatures() {
		if feature.Name == name {
			return feature.Feature, true
		}
	}
	return 0, false
}

func buildCoreMask(core string) (wago.CoreFeatures, error) {
	switch core {
	case "", "auto":
		var all wago.CoreFeatures
		for _, feature := range buildFeatures() {
			all |= feature.Feature
		}
		return all, nil
	case "1.0":
		return wago.CoreFeaturesV1, nil
	case "2.0":
		return wago.CoreFeaturesV2, nil
	case "3.0":
		return wago.CoreFeaturesV3 | relaxedSIMD, nil
	default:
		return 0, fmt.Errorf("unknown build.core %q (want: auto, 1.0, 2.0, or 3.0)", core)
	}
}

func (p *BuildProfile) validate() error {
	if p == nil {
		return nil
	}
	if _, err := buildCoreMask(p.core()); err != nil {
		return err
	}
	for name := range p.features() {
		if _, ok := lookupBuildFeature(name); !ok {
			return fmt.Errorf("unknown build feature %q", name)
		}
	}
	return nil
}

// FeaturesMask resolves the Core release and proposal overrides.
func (p *BuildProfile) FeaturesMask() (wago.CoreFeatures, error) {
	if err := p.validate(); err != nil {
		return 0, err
	}
	enabled, _ := buildCoreMask(p.core())
	var disabled wago.CoreFeatures
	for name, value := range p.features() {
		feature, _ := lookupBuildFeature(name)
		enabled = enabled.SetEnabled(feature, value)
		if !value {
			disabled |= feature
		}
	}
	if disabled.IsEnabled(wago.CoreFeatureExtendedConst) {
		if p.features()["extended-const-expressions"] {
			return 0, fmt.Errorf("extended-const-expressions requires extended-constant-expressions")
		}
		enabled &^= wago.CoreFeatureExtendedConstExpressions
	}
	if !enabled.IsEnabled(wago.CoreFeatureSIMD) {
		if p.features()["relaxed-simd"] {
			if disabled.IsEnabled(wago.CoreFeatureSIMD) {
				return 0, fmt.Errorf("relaxed-simd requires simd")
			}
			enabled |= wago.CoreFeatureSIMD
		} else {
			enabled &^= relaxedSIMD
		}
	}
	// Both typed references and GC use the reference-types instruction
	// family. GC also uses the exact typed-reference representation.
	if enabled.IsEnabled(wago.CoreFeatureGC) {
		enabled |= wago.CoreFeatureTypedFunctionReferences
	}
	if enabled.IsEnabled(wago.CoreFeatureTypedFunctionReferences) {
		enabled |= wago.CoreFeatureReferenceTypes
	}
	if conflict := enabled & disabled; conflict != 0 {
		return 0, fmt.Errorf("cannot disable required build feature(s) %s", conflict)
	}
	return enabled, nil
}

// Tags returns Go and TinyGo build constraints for the effective profile.
func (p *BuildProfile) Tags() ([]string, error) {
	if p.empty() {
		return nil, nil
	}
	enabled, err := p.FeaturesMask()
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, feature := range buildFeatures() {
		if !enabled.IsEnabled(feature.Feature) {
			tags = append(tags, "wago_no_"+strings.ReplaceAll(feature.Name, "-", "_"))
		}
	}
	sort.Strings(tags)
	return tags, nil
}

// BuildTagsForManifest uses a caller's locked project snapshot without
// reacquiring the project metadata lock during runtime publication.
func BuildTagsForManifest(manifest map[string]any) ([]string, error) {
	global, err := Load()
	if err != nil {
		return nil, err
	}
	local, _, err := decodeLocalLayer(manifest[localField])
	if err != nil {
		return nil, err
	}
	profile := global.Build
	if local.Build != nil {
		profile = mergeBuildProfile(profile, local.Build)
	}
	return profile.Tags()
}

func setBuildProfile(config *Config, key, value string) error {
	if config.Build == nil {
		config.Build = &BuildProfile{Features: make(map[string]bool)}
	}
	if key == "build.core" {
		if value == "inherit" {
			value = ""
		}
		if _, err := buildCoreMask(value); err != nil {
			return err
		}
		config.Build.Core = value
		return nil
	}
	name, ok := strings.CutPrefix(key, "build.features.")
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	if _, ok := lookupBuildFeature(name); !ok {
		return fmt.Errorf("unknown build feature %q", name)
	}
	if value == "inherit" {
		delete(config.Build.Features, name)
		return nil
	}
	enabled, err := ParseBool(value)
	if err != nil {
		return err
	}
	if config.Build.Features == nil {
		config.Build.Features = make(map[string]bool)
	}
	config.Build.Features[name] = enabled
	return nil
}

func getBuildProfile(profile *BuildProfile, key string) (string, error) {
	if key == "build.core" {
		if value := profile.core(); value != "" {
			return value, nil
		}
		return "inherit", nil
	}
	if name, ok := strings.CutPrefix(key, "build.features."); ok {
		if _, ok := lookupBuildFeature(name); ok {
			if enabled, ok := profile.features()[name]; ok {
				if enabled {
					return "true", nil
				}
				return "false", nil
			}
			return "inherit", nil
		}
	}
	return "", fmt.Errorf("unknown setting %q", key)
}
