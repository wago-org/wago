// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"sync"

	"github.com/wago-org/wago/src/core/compiler/optimization"
)

var optimizationBindingState struct {
	once     sync.Once
	bindings *optimization.Bindings
}

// Runtime admission shares this package with the compiler. Bind on the first
// compiler or knob request so importing admission never links the catalog.
func optimizationBindings() *optimization.Bindings {
	optimizationBindingState.once.Do(func() {
		nativeLeafHost, registerABI, preparedDirectEntry, preparedBoundedEntry := true, true, true, true
		optimizationBindingState.bindings = optimization.NewBindings("riscv64",
			optimization.BindingSpec{Name: "native-leaf-host", Value: &nativeLeafHost},
			optimization.BindingSpec{Name: "reg-abi", Value: &registerABI},
			optimization.BindingSpec{Name: "prepared-direct-entry", Value: &preparedDirectEntry},
			optimization.BindingSpec{Name: "prepared-bounded-entry", Value: &preparedBoundedEntry},
		)
	})
	return optimizationBindingState.bindings
}

func OptKnobs() []KnobInfo { return optimizationBindings().Infos() }

func OptKnobSnapshot() ([]KnobInfo, OptimizationSnapshot) {
	return optimizationBindings().Snapshot()
}

func CurrentOptKnobSnapshot() OptimizationSnapshot { return optimizationBindings().CurrentSnapshot() }
func SetOptKnob(name string, on bool) bool         { return optimizationBindings().Set(name, on) }
