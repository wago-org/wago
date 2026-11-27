// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !arm64

package wago

import "github.com/wago-org/wago/src/core/runtime/abi"

// The non-ARM64 shared dispatch path preserves the common GPR prefix.
type parkedHostRegisters [abi.AMD64SyncHostCallSavedBytes]byte
