// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "github.com/wago-org/wago/internal/wagopaths"

// Dirs is retained as a public alias while its implementation lives in a
// runtime-independent package used by the standard-Go version manager.
type Dirs = wagopaths.Dirs

func DirsFor(version string) Dirs { return wagopaths.DirsFor(version) }
