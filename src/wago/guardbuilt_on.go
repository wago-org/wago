// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (darwin && arm64) || (wago_guardpage && (((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)))

package wago

// guardPageBuilt is true on Darwin ARM64 and in supported builds compiled with
// -tags wago_guardpage, enabling signals-based (guard-page) bounds checks.
const guardPageBuilt = true
