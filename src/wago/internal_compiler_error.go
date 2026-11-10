// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot"

// InternalCompilerError identifies a recovered native code-generator panic.
// Use errors.As on a compilation error to distinguish it from rejected Wasm.
// It includes backend, function, bytecode context, the original panic, and a
// bounded Go stack snapshot (the snapshot is omitted in TinyGo builds).
type InternalCompilerError = railshot.InternalCompilerError
