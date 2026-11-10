// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo

package wago

import "testing"

func requireStandardGoTestRuntime(t *testing.T) bool {
	t.Helper()
	t.Log("test depends on standard Go testing control flow or struct layout")
	return false
}
