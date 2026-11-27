// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone || !wago_standalone_no_plugins

package wago

import (
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/optimization"
)

type railshotOptimizationSelection = optimization.Selection

func (c *RuntimeConfig) initializeOptimizationDefaults() {
	forceBMI2 := runtime.GOARCH == "amd64" && hostSupportsBMI2() && os.Getenv("WAGO_AMD64_NO_BMI2_RORX") != "1"
	c.optimizations, c.optimizationSnapshot, c.optimizationDeltas, c.defaultSelection = defaultOptimizationSnapshot(forceBMI2)
}

func (c *RuntimeConfig) validateOptimizations() error {
	if c.trustedOptimizations {
		return nil
	}
	for name := range c.optimizations {
		if !optimization.Exists(runtime.GOARCH, name) {
			return fmt.Errorf("wago: unknown %s optimization %q", runtime.GOARCH, name)
		}
	}
	return nil
}

var defaultOptimizationCache struct {
	sync.Mutex
	snapshot   railshotOptimizationSnapshot
	selection  optimization.Selection
	values     map[string]bool
	bmi2Values map[string]bool
	bmi2Delta  map[string]bool
}

// defaultOptimizationSnapshot returns an immutable process-default selection.
// The cache owns the maps: callers only read them, and WithOptimization clones
// before mutation. A backend revision change rebuilds the snapshot from values
// captured under the same lock as SetOptKnob.
func defaultOptimizationSnapshot(forceBMI2 bool) (values map[string]bool, snapshot railshotOptimizationSnapshot, deltas map[string]bool, selection optimization.Selection) {
	defaultOptimizationCache.Lock()
	defer defaultOptimizationCache.Unlock()

	snapshot = railshotCurrentOptKnobSnapshot()
	if defaultOptimizationCache.values == nil || defaultOptimizationCache.snapshot != snapshot {
		infos, capturedSnapshot := railshotOptKnobSnapshot()
		values = make(map[string]bool, len(infos))
		for _, info := range infos {
			values[info.Name] = info.On
		}
		defaultOptimizationCache.snapshot = capturedSnapshot
		defaultOptimizationCache.values = values
		// ARM64 consumes a packed copy of the complete immutable map.
		// Resolve only when the process defaults are first captured or change.
		if runtime.GOARCH == "arm64" {
			defaultOptimizationCache.selection, _ = capturedSnapshot.ResolveSelection(values)
		}
		defaultOptimizationCache.bmi2Values = nil
		defaultOptimizationCache.bmi2Delta = nil
		snapshot = capturedSnapshot
	}
	values = defaultOptimizationCache.values
	snapshot = defaultOptimizationCache.snapshot
	if !forceBMI2 || values["bmi2-rorx"] {
		return values, snapshot, nil, defaultOptimizationCache.selection
	}
	if defaultOptimizationCache.bmi2Values == nil {
		bmi2Values := make(map[string]bool, len(values))
		for name, on := range values {
			bmi2Values[name] = on
		}
		bmi2Values["bmi2-rorx"] = true
		defaultOptimizationCache.bmi2Values = bmi2Values
		defaultOptimizationCache.bmi2Delta = map[string]bool{"bmi2-rorx": true}
	}
	return defaultOptimizationCache.bmi2Values, snapshot, defaultOptimizationCache.bmi2Delta, optimization.Selection{}
}
