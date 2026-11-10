// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package self

import "strings"

func Channel(current string) string {
	if channel := pinnedChannel(current); channel != "" {
		return channel
	}
	if rolling(current) {
		return current
	}
	if strings.HasPrefix(current, "v") {
		return "latest"
	}
	return "canary"
}

func pinnedChannel(version string) string {
	for _, channel := range []string{"canary", "beta"} {
		if strings.Contains(version, "-"+channel+".") || strings.HasPrefix(version, channel+"@") {
			return channel
		}
	}
	return ""
}

func rolling(version string) bool {
	return version == "canary" || version == "beta"
}
