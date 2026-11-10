// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package version

import "testing"

func TestDiagnosticChannel(t *testing.T) {
	for _, test := range []struct {
		active, release, want string
	}{
		{"canary", "deadbee", "canary"},
		{"", "v0.1.0-canary.gdeadbee", "canary"},
		{"", "canary@deadbee123456789012345678901234567890123", "canary"},
		{"beta", "deadbee", "beta"},
		{"", "v0.1.0-beta.1", "beta"},
		{"", "beta@deadbee123456789012345678901234567890123", "beta"},
		{"latest", "v1.0.0", "latest"},
		{"", "v1.0.0", "stable"},
		{"local", "deadbee", "local"},
		{"", "deadbee", "development"},
	} {
		if got := DiagnosticChannel(test.active, test.release); got != test.want {
			t.Fatalf("DiagnosticChannel(%q, %q) = %q, want %q", test.active, test.release, got, test.want)
		}
	}
}
