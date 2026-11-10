// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo

package wago

// TinyGo's fixed task stack needs a smaller bound. Reject the fourth nested
// reentry before saved callers and native boundary frames reach the canary.
const maxHostReentryDepth = 3
