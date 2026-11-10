// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package self

import "testing"

func TestChannelPreservesReleaseTrack(t *testing.T) {
	tests := map[string]string{
		"canary":                 "canary",
		"v0.1.0-canary.g7d8c58a": "canary",
		"canary@7d8c58a000000000000000000000000000000000": "canary",
		"beta":          "beta",
		"v0.1.0-beta.2": "beta",
		"beta@7d8c58a000000000000000000000000000000000": "beta",
		"v0.2.0":  "latest",
		"0.0.0":   "canary",
		"7d8c58a": "canary",
	}
	for version, want := range tests {
		if got := Channel(version); got != want {
			t.Errorf("Channel(%q) = %q, want %q", version, got, want)
		}
	}
}
