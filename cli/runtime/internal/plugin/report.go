// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package plugin

import "github.com/wago-org/wago"

// Report is built only from immutable provider metadata and the side-effect-free
// plan. It never constructs, validates config with, registers, or starts code.
type Report struct {
	Definition wago.PluginDefinition `json:"definition"`
	Plan       *wago.PluginPlanEntry `json:"plan,omitempty"`
}

func BuildReport(definition wago.PluginDefinition, plan *wago.PluginPlanEntry) Report {
	return Report{Definition: definition, Plan: plan}
}
