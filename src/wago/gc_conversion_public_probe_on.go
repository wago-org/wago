// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_test_conversion_hooks

package wago

// Test hooks are installed before goroutine launch and removed after joins.
// The dedicated build tag keeps hook checks out of normal execution.
var conversionPublicFalseHook func(*Instance)
var conversionPublicEnableHook func()

func conversionPublicFalseObservation(in *Instance) {
	if conversionPublicFalseHook != nil {
		conversionPublicFalseHook(in)
	}
}
func conversionPublicEnablementObservation() {
	if conversionPublicEnableHook != nil {
		conversionPublicEnableHook()
	}
}
