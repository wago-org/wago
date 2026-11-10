// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package profilecmd

import "flag"

// FlagSet describes the same flags consumed by Run, for CLI help and discovery.
// Callers must not rely on parsing this independent set to configure execution.
func FlagSet(name string) *flag.FlagSet {
	switch name {
	case "record", "capture":
		f, _ := recordFlags(name)
		return f
	case "top", "annotate", "diff":
		f, _ := reportFlags(name)
		return f
	case "timeline":
		f, _ := timelineFlags()
		return f
	default:
		return nil
	}
}
