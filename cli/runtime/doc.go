// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package runtime implements commands backed by a compiled Wago engine.
//
// It contains no release networking or toolchain management. The manager
// selects the runtime artifact, including any compiled plugins, before launch.
package runtime
