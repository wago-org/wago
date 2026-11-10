// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package ir contains Wago's off-path scalar SSA research and verification
// representation. It is not an execution tier: production decode, validation,
// compilation, instantiation, and execution must not import this package.
//
// Keep the package bounded to concrete oracle, debugging, or shape-verification
// uses. Do not expand its feature surface merely to mirror production Railshot.
package ir
