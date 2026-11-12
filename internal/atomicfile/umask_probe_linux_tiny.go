// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && tinygo && wago_lean && wago_minimal

package atomicfile

import "io/fs"

// ApplyUmask is used by the build command, which is deliberately absent from
// the TinyGo minimal runtime. Reject an unexpected request rather than linking
// its descriptor-pinned probe machinery or silently changing file semantics.
func probeUmaskMode(string, fs.FileMode, bool) (fs.FileMode, error) {
	return 0, newError("atomic umask probing is unavailable")
}
