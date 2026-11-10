// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package version

import "strings"

func diagnosticChannel(activeVersion, release string) string {
	switch {
	case activeVersion == "canary", strings.Contains(release, "-canary.g"), strings.HasPrefix(release, "canary@"):
		return "canary"
	case activeVersion == "beta", strings.Contains(release, "-beta."), strings.HasPrefix(release, "beta@"):
		return "beta"
	case activeVersion == "latest":
		return "latest"
	case strings.HasPrefix(release, "v"):
		return "stable"
	case activeVersion != "":
		return activeVersion
	default:
		return "development"
	}
}
