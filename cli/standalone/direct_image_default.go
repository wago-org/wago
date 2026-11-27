// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone || !wago_standalone_no_plugins || !wago_standalone_generated

package standalone

import "github.com/wago-org/wago"

const standaloneDirectImage = false
const standaloneGuestExit = true

func executeDirectImage(*wago.Compiled, *wago.RuntimeConfig, string, []string) error {
	panic("direct standalone image requires a generated profile")
}
