// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package install

// EnsureRuntime starts installation when no active runtime exists and reports
// whether the caller can continue with the original invocation.
func EnsureRuntime(hasActive func() bool, install func()) bool {
	if hasActive() {
		return true
	}
	install()
	return hasActive()
}
