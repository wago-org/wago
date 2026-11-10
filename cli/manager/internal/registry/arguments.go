// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package registry

import "strings"

func splitVersion(value string) (name, version string) {
	if index := strings.LastIndexByte(value, '@'); index >= 0 {
		return value[:index], value[index+1:]
	}
	return value, ""
}
