// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone && wago_standalone_no_plugins

package wago

import "errors"

// Bare executables adopt an image with the codegen decisions already made.
type railshotOptimizationSelection struct{}
type railshotOptimizationSnapshot struct{}

func (*RuntimeConfig) initializeOptimizationDefaults() {}

func (c *RuntimeConfig) validateOptimizations() error {
	if len(c.optimizations) != 0 {
		return errors.New("wago: standalone codegen options must be set when creating the native image")
	}
	return nil
}
