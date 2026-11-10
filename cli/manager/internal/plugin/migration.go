// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package plugin

import "github.com/wago-org/wago/internal/wagopaths"

// sharedGlobalPluginDir is the one v1 global intent directory. V0 per-version
// manifests are deliberately not migrated or interpreted.
func sharedGlobalPluginDir(d wagopaths.Dirs) string { return d.Data }
