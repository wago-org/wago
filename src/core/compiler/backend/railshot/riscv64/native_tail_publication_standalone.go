// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && wago_standalone

package riscv64

// Standalone dispatch only borrows code published by the runtime owner.
func (f *tailRAFunction) publishCode() error {
	if f.code != nil || f.published == nil {
		return unsupported("native tail exact published code owner")
	}
	return f.published.Validate()
}
