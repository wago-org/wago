// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"runtime"
	"testing"
)

// Statistics assertions require a diagnostic build. CI runs both the ordinary
// runtime suite and this suite with wago_codegenstats so neither path is hidden.
// The result must be checked: TinyGo's SkipNow does not stop execution.
func requireCompilerDiagnostics(t testing.TB) bool {
	t.Helper()
	if !compilerTelemetryEnabled {
		const reason = "compiler diagnostics require -tags=wago_codegenstats (also enabled by wago_profile)"
		if runtime.Compiler == "tinygo" {
			// TinyGo SkipNow also marks the test failed; log and let the caller return.
			t.Log(reason)
		} else {
			t.Skip(reason)
		}
		return false
	}
	return true
}
