// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package plugin

import (
	"sort"
	"strings"
)

func compiledPluginSummary() string {
	definitions := Definitions()
	if len(definitions) == 0 {
		return "none"
	}
	plugins := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		label := definition.ID
		if version := strings.TrimSpace(definition.Version); version != "" {
			label += "@" + version
		}
		plugins = append(plugins, label)
	}
	sort.Strings(plugins)
	return strings.Join(plugins, ", ")
}
