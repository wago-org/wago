// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_collector || (wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling))

package wago

const collectorObjectTokensBuilt = false

type parkedHostRootCache struct{}

func (*parkedHostRootCache) parkedRoots(uint8) *parkedHostRoots { return nil }

func (*parkedHostRootCache) ensureParkedRoots(uint8) *parkedHostRoots {
	panic("parked host object roots are disabled in this build")
}
