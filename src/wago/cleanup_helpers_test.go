// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

func cleanupTestPanic(fn func()) (value any, panicked bool) {
	returned := false
	defer func() {
		if !returned {
			value = recover()
			panicked = true
		}
	}()
	fn()
	returned = true
	return nil, false
}
