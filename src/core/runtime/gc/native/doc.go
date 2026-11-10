// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package gc at the native import path is the trusted WasmGC execution ABI.
// Compact Ref words are collector-local indexes, with no ownership/generation
// proof. JIT adapters must validate Go/public tokens before converting them to
// these words. Normal Go callers must use the checked parent gc package instead.
// NativeView, prevalidated operations and direct root words are internal runtime
// mechanisms, not general-purpose reference ingress or memory safety boundaries.
package gc
