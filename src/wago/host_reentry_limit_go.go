// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo

package wago

// Standard Go grows its host stack, allowing nested component adapter chains.
// Keep native stacks and call buffers bounded even when the host stack can grow.
const maxHostReentryDepth = 32
