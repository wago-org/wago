// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build ((linux || darwin) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

func numericContextDetached(in *Instance) bool {
	p := in.eng.PreparedScalarHost()
	return p != nil && p.DetachedNumericContext()
}
