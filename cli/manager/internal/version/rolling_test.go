// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package version

import "testing"

func TestIsRollingChannel(t *testing.T) {
	for _, v := range []string{"canary", "beta"} {
		if !isRollingChannel(v) {
			t.Errorf("%q should be a rolling channel", v)
		}
	}
	for _, v := range []string{"1.2.3", "v1.0.0", "stable", "", "canaryx", "night"} {
		if isRollingChannel(v) {
			t.Errorf("%q should not be a rolling channel", v)
		}
	}
}
