// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package plugin

func ScopeLabel() string {
	return selectedPluginScopeLabel()
}

func selectedPluginScopeLabel() string {
	environment, err := resolvePluginEnvironment()
	if err != nil {
		return "active scope"
	}
	if environment.scope == "plain" {
		return "global"
	}
	return environment.scope
}
