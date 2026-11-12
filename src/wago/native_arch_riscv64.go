// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

// Identity4 adds authenticated invocation views to the framed, cooperative
// RV64G ABI. Older RV64 images lack the required entry/selector protocol and
// cannot be combined with future public tails. Other target identities remain
// unchanged. No native pointer is persisted in an artifact.
const compiledArchitecture byte = 4
