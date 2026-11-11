// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "runtime"

// Collector algorithms operate on the shared checked handle and object layout.
// Both native backends use this layout; instruction selection remains separate.
const nativeCollectorFastPaths = runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
