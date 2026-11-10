// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_target_tinygo || wago_standalone

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func railshotExceptionHandlingSource(m *wasm.Module) bool { return false }

func railshotManagedExceptionReferences(*wasm.Module) bool { return false }
