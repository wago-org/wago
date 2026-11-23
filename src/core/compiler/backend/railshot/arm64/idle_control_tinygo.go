// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo

package arm64

// Retaining control storage is useful on the standard Go allocator. TinyGo
// keeps the direct reset path for small-module compilation.
const retainIdleControlStorage = false
