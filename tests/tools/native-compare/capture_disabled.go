// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_nativecompare && (!wago_profile || !amd64)

package main

import "fmt"

func captureCommand(input, output string) {
	fail(fmt.Errorf("capture requires an AMD64 build with -tags=wago_profile; comparison does not require the profile tag"))
}
