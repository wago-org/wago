// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && !wago_regalloccheck && !wago_standalone

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Call the same backend directly to avoid two nested forwarding wrappers
// exceeding the ordinary Go inlining budget.
func railshotCompileValidatedModuleWith(m *wasm.Module, options railshotCompileOptions, _ *wasm.ValidatedModuleAnalysis, _ wasm.ValidationFeatures) (*railshotCompiledModule, error) {
	return railshot.CompileModuleWith(m, options)
}
