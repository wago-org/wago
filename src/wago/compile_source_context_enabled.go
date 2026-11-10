// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_regalloccheck

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// This handoff preserves the original validation profile, before frontend
// narrowing. Options stays compile-scoped; workers borrow immutable facts.
func railshotCompileValidatedModuleWith(m *wasm.Module, options railshotCompileOptions, analysis *wasm.ValidatedModuleAnalysis, features wasm.ValidationFeatures) (*railshotCompiledModule, error) {
	options.Codegen = codegen.SourceOptions(options.Codegen, m, analysis, features)
	defer codegen.CloseSourceContext(options.Codegen, m)
	return railshotCompileModuleWith(m, options)
}
