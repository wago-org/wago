// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "testing"

// Statistics assertions require a diagnostic build. CI runs both the ordinary
// runtime suite and this suite with wago_codegenstats so neither path is hidden.
func requireCompilerDiagnostics(t testing.TB) {
	t.Helper()
	if !diagnosticsEnabled {
		t.Skip("compiler diagnostics require -tags=wago_codegenstats (also enabled by wago_profile)")
	}
}

// optionalTestStats keeps semantic execution checks active in ordinary builds.
// Callers guard only the counter assertions with diagnosticsEnabled.
func optionalTestStats(stats *ModuleStats) *ModuleStats {
	if diagnosticsEnabled {
		return stats
	}
	return nil
}
