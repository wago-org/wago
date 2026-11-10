// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (!linux && !darwin && !windows) || (!amd64 && !arm64) || tinygo

package wago

import rt "github.com/wago-org/wago/src/core/runtime"

const gcI31InductionSupported = false

//go:noinline
func clearPreparedInductionTrap(in *Instance) error {
	rt.PreparePreparedIntTrap(in.trap)
	if rt.PreparedIntTrapCode(in.trap) != rt.TrapNone {
		return rt.ConsumePreparedIntTrap(in.trap)
	}
	return nil
}
