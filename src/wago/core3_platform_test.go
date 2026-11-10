// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	goruntime "runtime"
	"testing"
)

func requireCompleteCore3Backend(t testing.TB) {
	t.Helper()
	if !supportsCompleteCore3Backend(goruntime.GOOS, goruntime.GOARCH) {
		t.Skipf("complete Core 3 execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
}
